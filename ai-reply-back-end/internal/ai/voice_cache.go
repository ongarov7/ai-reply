package ai

import (
	"context"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

const (
	// voiceCacheTTL — сәтті нәтиже осынша уақыт сол request_id-ге қайта беріледі.
	voiceCacheTTL = 10 * time.Minute
	// voiceCacheEntries — жадтағы аяқталған нәтижелердің шегі.
	voiceCacheEntries = 10000
	// voiceCacheSweep — мерзімі өткен жазбаларды тазалаудың әдеттегі аралығы.
	voiceCacheSweep = time.Minute
)

// voiceCache — бір (қолданушы, request_id) жұбына бір провайдер шақыруы.
//
// The keyboard re-sends an upload when the network flickers, and paying for
// the same recording twice is exactly what request_id prevents. A request
// whose id is already in flight waits for that call instead of making its
// own; a successful result is handed to repeats for voiceCacheTTL; a failure
// is forgotten at once, so the next attempt asks the provider again. When the
// caller that runs the call goes away (its context is cancelled), whoever was
// waiting runs it again rather than inheriting a cancellation that was not
// theirs.
//
// Memory is bounded: expired results are swept, and at voiceCacheEntries the
// result closest to expiry is dropped first. Calls in flight are never
// dropped (someone may be waiting on them); there are only as many as there
// are requests running. In memory, like the rate limiter: one instance, not
// shared between replicas.
type voiceCache struct {
	mu      sync.Mutex
	entries map[string]*voiceEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
	sweptAt time.Time
}

// voiceEntry — бір кілттің шақыруы. expires нөл болса — әлі жүріп жатыр.
type voiceEntry struct {
	done    chan struct{}
	result  TranscribeResult
	err     error
	expires time.Time
	// abandoned — шақырған сұраныстың контексі тоқтады: күтушілер өздері қайталайды.
	abandoned bool
}

func newVoiceCache(ttl time.Duration, maxEntries int, now func() time.Time) *voiceCache {
	return &voiceCache{entries: map[string]*voiceEntry{}, ttl: ttl, max: maxEntries, now: now}
}

// do — кілттің нәтижесі: жадтан, жүріп жатқан шақырудан не call-дың өзінен.
// reused=true — call бұл сұраныс үшін шақырылмады.
func (c *voiceCache) do(ctx context.Context, key string,
	call func() (TranscribeResult, error)) (result TranscribeResult, reused bool, err error) {
	for {
		c.mu.Lock()
		now := c.now()
		c.sweep(now, false)
		e, ok := c.entries[key]
		if ok && !e.expires.IsZero() && !now.Before(e.expires) {
			delete(c.entries, key)
			ok = false
		}
		if !ok {
			break // мьютекс ұсталып тұр: шақыру осы сұранысқа тиесілі
		}
		if !e.expires.IsZero() {
			c.mu.Unlock()
			return e.result, true, nil
		}
		c.mu.Unlock()
		select {
		case <-e.done:
		case <-ctx.Done():
			return TranscribeResult{}, false, cancelled(ctx)
		}
		if e.err == nil {
			return e.result, true, nil
		}
		if !e.abandoned {
			return TranscribeResult{}, false, e.err
		}
	}

	e := &voiceEntry{done: make(chan struct{})}
	c.makeRoom()
	c.entries[key] = e
	c.mu.Unlock()

	// A panic in call still releases the waiters (with an error) before it
	// travels on to the Recover middleware.
	err = domain.ErrProviderDown
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		e.result, e.err = result, err
		if err == nil {
			e.expires = c.now().Add(c.ttl)
		} else {
			e.abandoned = ctx.Err() != nil
			if c.entries[key] == e {
				delete(c.entries, key)
			}
		}
		close(e.done)
	}()
	result, err = call()
	return result, false, err
}

// makeRoom — шекке жеткенде мерзімі өткендерді, одан кейін ең ескі аяқталған нәтижені шығарады.
// Мьютекс ұсталып тұруы тиіс.
func (c *voiceCache) makeRoom() {
	if len(c.entries) < c.max {
		return
	}
	c.sweep(c.now(), true)
	for len(c.entries) >= c.max {
		var oldest string
		var oldestAt time.Time
		for key, e := range c.entries {
			if !e.expires.IsZero() && (oldest == "" || e.expires.Before(oldestAt)) {
				oldest, oldestAt = key, e.expires
			}
		}
		if oldest == "" {
			return // бәрі жүріп жатыр
		}
		delete(c.entries, oldest)
	}
}

// sweep — мерзімі өткен нәтижелерді жояды: force болмаса, voiceCacheSweep сайын бір рет.
func (c *voiceCache) sweep(now time.Time, force bool) {
	if !force && now.Sub(c.sweptAt) < voiceCacheSweep {
		return
	}
	for key, e := range c.entries {
		if !e.expires.IsZero() && !now.Before(e.expires) {
			delete(c.entries, key)
		}
	}
	c.sweptAt = now
}

// size — жадтағы жазбалар саны (тестке).
func (c *voiceCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// cancelled — күтіп тұрған сұраныстың өз контексі тоқтады.
func cancelled(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return domain.ErrProviderTimeout
	}
	return domain.ErrProviderDown
}
