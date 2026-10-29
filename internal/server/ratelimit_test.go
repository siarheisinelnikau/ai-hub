package server

import (
	"testing"
	"time"
)

func TestLoginLimiterLockout(t *testing.T) {
	l := newLoginLimiter(3, time.Minute, 30*time.Second)

	// 2 failures (below threshold): still allowed
	l.recordFailure("1.2.3.4")
	l.recordFailure("1.2.3.4")
	if allowed, _ := l.check("1.2.3.4"); !allowed {
		t.Fatal("should be allowed after 2 failures (threshold 3)")
	}

	// 3rd failure: locks the key
	l.recordFailure("1.2.3.4")
	if allowed, ra := l.check("1.2.3.4"); allowed {
		t.Fatal("should be locked after 3 failures")
	} else if ra < 1 {
		t.Fatalf("retry-after should be >= 1s, got %d", ra)
	}

	// other key unaffected
	if allowed, _ := l.check("5.6.7.8"); !allowed {
		t.Fatal("a different IP should be unaffected")
	}

	// reset clears the lock
	l.reset("1.2.3.4")
	if allowed, _ := l.check("1.2.3.4"); !allowed {
		t.Fatal("should be allowed again after reset")
	}
}

func TestLoginLimiterWindowReset(t *testing.T) {
	l := newLoginLimiter(2, 50*time.Millisecond, time.Second)
	l.recordFailure("ip")
	time.Sleep(60 * time.Millisecond) // window expires
	l.recordFailure("ip") // counter reset by window expiry
	if allowed, _ := l.check("ip"); !allowed {
		t.Fatal("failures outside the window should not lock")
	}
}
