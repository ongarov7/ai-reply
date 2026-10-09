package middleware

import (
	"fmt"
	"testing"
	"time"
)

// testLimiter — сағаты тесттен басқарылатын лимитер.
func testLimiter() (*Limiter, *time.Time) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	l := NewLimiter()
	l.lastGC = now
	l.now = func() time.Time { return now }
	return l, &now
}

// Минуттық шелек іске қосқан тазалау сағаттық шелекті өз терезесімен ғана тазалайды.
func TestCleanupKeepsHourlyBucketsAlive(t *testing.T) {
	l, now := testLimiter()
	for i := 0; i < 2; i++ {
		if ok, _ := l.Allow("otp_request:10.0.0.1", 2, time.Hour); !ok {
			t.Fatalf("hourly hit %d refused", i+1)
		}
	}
	*now = now.Add(11 * time.Minute) // past the cleanup interval
	if ok, _ := l.Allow("generic:10.0.0.2", 60, time.Minute); !ok {
		t.Fatal("per-minute hit refused")
	}
	if l.lastGC != *now {
		t.Fatal("the per-minute call did not run the cleanup")
	}
	ok, retry := l.Allow("otp_request:10.0.0.1", 2, time.Hour)
	if ok {
		t.Fatal("the hourly bucket was wiped by a per-minute cleanup")
	}
	if retry != 49*time.Minute {
		t.Fatalf("retry = %v, want 49m", retry)
	}

	// After its own hour the bucket is empty and collected.
	*now = now.Add(50 * time.Minute)
	if ok, _ := l.Allow("generic:10.0.0.2", 60, time.Minute); !ok {
		t.Fatal("per-minute hit refused")
	}
	if _, kept := l.buckets["otp_request:10.0.0.1"]; kept {
		t.Fatal("an expired hourly bucket is kept")
	}
	if ok, _ := l.Allow("otp_request:10.0.0.1", 2, time.Hour); !ok {
		t.Fatal("a new hour must allow again")
	}
}

// Кілт шектен асса да, тазалау әр сұраныста емес, ең көбі gcMinInterval сайын жүреді.
func TestCleanupFrequencyIsBounded(t *testing.T) {
	l, now := testLimiter()
	l.maxKeys = 3
	for i := 0; i < 5; i++ {
		l.Allow(fmt.Sprintf("generic:%d", i), 10, time.Hour)
	}
	gc := l.lastGC
	*now = now.Add(time.Second)
	l.Allow("generic:5", 10, time.Hour)
	if l.lastGC != gc {
		t.Fatal("cleanup ran again within the minimum interval")
	}
	*now = now.Add(gcMinInterval)
	l.Allow("generic:6", 10, time.Hour)
	if l.lastGC != *now {
		t.Fatal("over the key limit, cleanup runs once the minimum interval has passed")
	}
	if len(l.buckets) != 7 {
		t.Fatalf("live hourly buckets were dropped: %d", len(l.buckets))
	}
}

func TestAllowCountsWithinTheWindow(t *testing.T) {
	l, now := testLimiter()
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k", 3, time.Minute); !ok {
			t.Fatalf("hit %d refused", i+1)
		}
		*now = now.Add(10 * time.Second)
	}
	if ok, retry := l.Allow("k", 3, time.Minute); ok || retry != 30*time.Second {
		t.Fatalf("fourth hit: ok %v retry %v", ok, retry)
	}
	*now = now.Add(31 * time.Second)
	if ok, _ := l.Allow("k", 3, time.Minute); !ok {
		t.Fatal("the oldest hit left the window")
	}
	if ok, _ := l.Allow("off", 0, time.Minute); !ok {
		t.Fatal("limit 0 means no limit")
	}
}
