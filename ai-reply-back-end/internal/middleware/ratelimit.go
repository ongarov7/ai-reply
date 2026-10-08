package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Limiter — жадтағы жылжымалы терезе.
//
// In memory is the right call for a single-instance deployment and the wrong
// call behind more than one replica: swap this type for Redis and nothing else
// changes. Saying that plainly beats shipping something that looks distributed
// and is not.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
	maxKeys int
	now     func() time.Time
}

// bucket — бір кілттің соққылары және оның өз терезесі.
//
// The window is stored with the hits: cleanup runs from whichever request
// comes along, and must not trim an hourly bucket with a per-minute window.
type bucket struct {
	window time.Duration
	hits   []time.Time
}

const (
	// gcInterval — ескі кілттерді тазалаудың әдеттегі аралығы.
	gcInterval = 10 * time.Minute
	// gcMinInterval — кілт саны шектен асса да, тазалау бұдан жиі жүрмейді
	// (әйтпесе әр сұраныс бүкіл кестені аралайды).
	gcMinInterval = 10 * time.Second
)

// NewLimiter — лимитер.
func NewLimiter() *Limiter {
	return &Limiter{buckets: make(map[string]*bucket), lastGC: time.Now(), maxKeys: 50000, now: time.Now}
}

// Allow — берілген кілт үшін терезеде орын бар ма.
func (l *Limiter) Allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if since := now.Sub(l.lastGC); since > gcInterval || (len(l.buckets) > l.maxKeys && since > gcMinInterval) {
		l.collect(now)
	}

	b := l.buckets[key]
	if b == nil {
		b = &bucket{window: window}
		l.buckets[key] = b
	}
	// A key always belongs to one rule; the widest window seen is kept.
	if window > b.window {
		b.window = window
	}
	b.hits = filter(b.hits, now, window)
	if len(b.hits) >= limit {
		retry := window - now.Sub(b.hits[0])
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry
	}
	b.hits = append(b.hits, now)
	return true, 0
}

// collect — әр кілтті өз терезесімен тазалайды; бос кілттер жойылады.
func (l *Limiter) collect(now time.Time) {
	for k, b := range l.buckets {
		if b.hits = filter(b.hits, now, b.window); len(b.hits) == 0 {
			delete(l.buckets, k)
		}
	}
	l.lastGC = now
}

func filter(values []time.Time, now time.Time, window time.Duration) []time.Time {
	out := values[:0]
	for _, t := range values {
		if now.Sub(t) < window {
			out = append(out, t)
		}
	}
	return out
}

// KeyFunc — лимит кілтін есептеу.
type KeyFunc func(*http.Request) string

// RateLimit — миддлварь түріндегі шектеу.
func RateLimit(l *Limiter, bucket string, limit int, window time.Duration, key KeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retry := l.Allow(bucket+":"+key(r), limit, window)
			if !ok {
				w.Header().Set("Retry-After", itoa(int(retry.Seconds())))
				httpx.Error(w, http.StatusTooManyRequests, httpx.CodeRateLimited,
					"Too many requests. Try again shortly.",
					map[string]any{"retry_after_seconds": int(retry.Seconds())})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func itoa(v int) string {
	if v <= 0 {
		return "1"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}
