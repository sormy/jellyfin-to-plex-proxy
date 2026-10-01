package main

import (
	"testing"
	"time"
)

func TestLoginThrottle(t *testing.T) {
	clock := time.Unix(0, 0)
	throttle := NewLoginThrottle(func() time.Time { return clock })
	fail := func(name string, times int) {
		for range make([]struct{}, times) {
			throttle.Failed(name)
		}
	}

	fail("Jellyfin", maxFailedLogins-1)
	if throttle.Locked("jellyfin") != 0 {
		t.Fatal("locked before the limit")
	}
	fail("JELLYFIN", 1)
	if throttle.Locked("jellyfin") != loginLockout {
		t.Fatalf("locked for %v after the limit, across cases", throttle.Locked("jellyfin"))
	}
	if throttle.Locked("someone-else") != 0 {
		t.Error("one name's failures locked another")
	}
	clock = clock.Add(loginLockout + time.Second)
	if throttle.Locked("jellyfin") != 0 {
		t.Error("still locked after the lockout")
	}

	fail("jellyfin", maxFailedLogins-1)
	throttle.Succeeded("jellyfin")
	fail("jellyfin", 1)
	if throttle.Locked("jellyfin") != 0 {
		t.Error("a success did not clear the count")
	}

	fail("quiet", maxFailedLogins-1)
	clock = clock.Add(loginLockout + time.Second)
	fail("quiet", 1)
	if throttle.Locked("quiet") != 0 {
		t.Error("failures a lockout apart still added up")
	}
}
