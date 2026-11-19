package screenshot

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Store — persistence. repository.Store satisfies it.
type Store interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSettings(ctx context.Context, values map[string]string) error
}

// Service — the credit policy in effect and its admin updates.
type Service struct {
	store    Store
	ttl      time.Duration
	now      func() time.Time
	mu       sync.RWMutex
	policy   Policy
	loadedAt time.Time
	loaded   bool
}

// New — reads the admin settings over the code defaults.
func New(store Store) *Service {
	return &Service{store: store, ttl: 5 * time.Second, now: time.Now}
}

// WithTTL — for tests that need every read to hit the store.
func (s *Service) WithTTL(ttl time.Duration) *Service { s.ttl = ttl; return s }

// Policy — the hot path, read on every estimate and screenshot reply. Cached
// for a few seconds; a failed read keeps the last known values, so a database
// hiccup never changes what a reply costs.
func (s *Service) Policy(ctx context.Context) Policy {
	if s == nil {
		return DefaultPolicy()
	}
	s.mu.RLock()
	if s.loaded && s.now().Sub(s.loadedAt) < s.ttl {
		p := s.policy
		s.mu.RUnlock()
		return p
	}
	s.mu.RUnlock()
	fresh, err := s.read(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.loaded {
			return s.policy
		}
		return DefaultPolicy()
	}
	s.policy, s.loadedAt, s.loaded = fresh, s.now(), true
	return fresh
}

// Update — admin panel: validates, stores atomically and refreshes the cache.
func (s *Service) Update(ctx context.Context, next Policy) (Policy, error) {
	if err := next.Validate(); err != nil {
		return Policy{}, err
	}
	if err := s.store.SetSettings(ctx, next.settings()); err != nil {
		return Policy{}, err
	}
	s.mu.Lock()
	s.policy, s.loadedAt, s.loaded = next, s.now(), true
	s.mu.Unlock()
	return next, nil
}

func (s *Service) read(ctx context.Context) (Policy, error) {
	out := DefaultPolicy()
	for _, f := range out.intFields() {
		raw, err := s.store.Setting(ctx, f.key)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return Policy{}, err
		}
		// A hand-edited row that does not parse or is out of range keeps the
		// default rather than silently changing what people pay.
		if v, convErr := strconv.Atoi(raw); convErr == nil && v >= f.rng.Min && v <= f.rng.Max {
			*f.target = v
		}
	}
	switch raw, err := s.store.Setting(ctx, SettingImageEnabled); {
	case err == nil:
		out.ImageEnabled = raw != "0" && raw != "false"
	case !errors.Is(err, domain.ErrNotFound):
		return Policy{}, err
	}
	switch raw, err := s.store.Setting(ctx, SettingShortcutURL); {
	case err == nil:
		if ValidShortcutURL(raw) {
			out.ShortcutURL = raw
		}
	case !errors.Is(err, domain.ErrNotFound):
		return Policy{}, err
	}
	return out, nil
}
