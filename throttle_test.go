package main

import (
	"testing"
	"time"
)

func TestLoginThrottle(t *testing.T) {
	clock := time.Unix(0, 0)
	throttle := NewLoginThrottle(func() time.Time { return clock }, defaultMaxFailedLogins, defaultLoginLockout)
	fail := func(name string, times int) {
		for range make([]struct{}, times) {
			throttle.Failed(name)
		}
	}

	fail("Jellyfin", defaultMaxFailedLogins-1)
	if throttle.Locked("jellyfin") != 0 {
		t.Fatal("locked before the limit")
	}
	fail("JELLYFIN", 1)
	if throttle.Locked("jellyfin") != defaultLoginLockout {
		t.Fatalf("locked for %v after the limit, across cases", throttle.Locked("jellyfin"))
	}
	if throttle.Locked("someone-else") != 0 {
		t.Error("one name's failures locked another")
	}
	clock = clock.Add(defaultLoginLockout + time.Second)
	if throttle.Locked("jellyfin") != 0 {
		t.Error("still locked after the lockout")
	}

	fail("jellyfin", defaultMaxFailedLogins-1)
	throttle.Succeeded("jellyfin")
	fail("jellyfin", 1)
	if throttle.Locked("jellyfin") != 0 {
		t.Error("a success did not clear the count")
	}

	fail("quiet", defaultMaxFailedLogins-1)
	clock = clock.Add(defaultLoginLockout + time.Second)
	fail("quiet", 1)
	if throttle.Locked("quiet") != 0 {
		t.Error("failures a lockout apart still added up")
	}
}

func TestLoginThrottleSettings(t *testing.T) {
	for _, tc := range []struct {
		name        string
		maxFailures int
		lockout     time.Duration
		failures    int
		locked      time.Duration
	}{
		{"custom limit reached", 2, time.Minute, 2, time.Minute},
		{"custom limit not reached", 3, time.Minute, 2, 0},
		{"off", 0, time.Minute, 100, 0},
	} {
		throttle := NewLoginThrottle(func() time.Time { return time.Unix(0, 0) }, tc.maxFailures, tc.lockout)
		for range make([]struct{}, tc.failures) {
			throttle.Failed("jellyfin")
		}
		if got := throttle.Locked("jellyfin"); got != tc.locked {
			t.Errorf("%s: locked %v, want %v", tc.name, got, tc.locked)
		}
	}
}
