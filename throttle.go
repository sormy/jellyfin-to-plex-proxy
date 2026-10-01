package main

import (
	"strings"
	"sync"
	"time"
)

const (
	maxFailedLogins = 5
	loginLockout    = 15 * time.Minute
)

// LoginThrottle locks a user name after repeated wrong passwords, so guessing
// one is slow; a success, or a quiet lockout period, clears the count.
type LoginThrottle struct {
	mu     sync.Mutex
	now    func() time.Time
	byName map[string]*loginFailures
}

type loginFailures struct {
	count       int
	last        time.Time
	lockedUntil time.Time
}

func NewLoginThrottle(now func() time.Time) *LoginThrottle {
	return &LoginThrottle{now: now, byName: map[string]*loginFailures{}}
}

// Locked tells how long a user name stays locked, zero when it is not.
func (t *LoginThrottle) Locked(name string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if f, ok := t.byName[strings.ToLower(name)]; ok {
		return max(f.lockedUntil.Sub(t.now()), 0)
	}
	return 0
}

func (t *LoginThrottle) Failed(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for key, f := range t.byName {
		if now.Sub(f.last) > loginLockout && now.After(f.lockedUntil) {
			delete(t.byName, key)
		}
	}
	key := strings.ToLower(name)
	f, ok := t.byName[key]
	if !ok {
		f = &loginFailures{}
		t.byName[key] = f
	}
	f.count++
	f.last = now
	if f.count >= maxFailedLogins {
		f.count, f.lockedUntil = 0, now.Add(loginLockout)
	}
}

func (t *LoginThrottle) Succeeded(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byName, strings.ToLower(name))
}
