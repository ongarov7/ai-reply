package aicontext

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Record — the stored document of one account.
type Record struct {
	Document Document
	// Revision — 0 until the first save; every save adds one.
	Revision  int64
	UpdatedAt time.Time
}

// RevisionError — the document changed since the client read it (409).
type RevisionError struct {
	Current int64
}

func (e *RevisionError) Error() string {
	return "ai context changed (revision " + strconv.FormatInt(e.Current, 10) + ")"
}

// Store — persistence; repository.Store satisfies it.
type Store interface {
	// AIContext — the raw JSON document; domain.ErrNotFound when never saved.
	AIContext(ctx context.Context, userID string) (raw string, revision int64, updatedAt time.Time, err error)
	// SaveAIContext — writes the document if the stored revision is still
	// expected (0 = no row yet). ok=false: someone saved first; current is
	// the revision now stored.
	SaveAIContext(ctx context.Context, userID, raw string, expected int64, now time.Time) (ok bool, current int64, err error)
	Setting(ctx context.Context, key string) (string, error)
	SetSettings(ctx context.Context, values map[string]string) error
}

// Service — reads and saves the document and the surcharge settings.
type Service struct {
	store Store
	now   func() time.Time
	ttl   time.Duration

	mu       sync.RWMutex
	pricing  Pricing
	loadedAt time.Time
	loaded   bool
}

// New — the service with a short settings cache (an admin change reaches every
// request within a few seconds).
func New(store Store) *Service {
	return &Service{store: store, now: time.Now, ttl: 5 * time.Second}
}

// WithClock — for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// WithTTL — for tests that need every read to hit the store.
func (s *Service) WithTTL(ttl time.Duration) *Service { s.ttl = ttl; return s }

// Get — the account's document; the default one before the first save.
func (s *Service) Get(ctx context.Context, userID string) (Record, error) {
	raw, revision, updated, err := s.store.AIContext(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return Record{Document: Default()}, nil
	}
	if err != nil {
		return Record{}, err
	}
	doc := Default()
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		// A row only this service writes; a broken one must not stop replies.
		return Record{Document: Default(), Revision: revision, UpdatedAt: updated}, nil
	}
	return Record{Document: doc.Complete(), Revision: revision, UpdatedAt: updated}, nil
}

// Save — validates and replaces the whole document if expected is still the
// stored revision. *FieldError for a refused field, *RevisionError for a
// concurrent change.
func (s *Service) Save(ctx context.Context, userID string, doc Document, expected int64) (Record, error) {
	if expected < 0 {
		return Record{}, &FieldError{Field: "expected_revision", Reason: ReasonUnknown}
	}
	clean, err := Normalize(doc)
	if err != nil {
		return Record{}, err
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	ok, current, err := s.store.SaveAIContext(ctx, userID, string(raw), expected, now)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return Record{}, &RevisionError{Current: current}
	}
	return Record{Document: clean, Revision: current, UpdatedAt: now.Truncate(time.Millisecond)}, nil
}

// ForRequest — what one generation adds to the prompt and the extra replies it
// costs on a plan that is not unlimited. A read error leaves the request
// without the context instead of failing it.
func (s *Service) ForRequest(ctx context.Context, userID, recipient string) (Rendered, int, error) {
	record, err := s.Get(ctx, userID)
	if err != nil {
		return Rendered{}, 0, err
	}
	rendered := record.Document.Render(recipient)
	if !rendered.Present() {
		return Rendered{}, 0, nil
	}
	return rendered, s.Pricing(ctx).Extra(rendered.Chars), nil
}

// Pricing — the surcharge settings in effect (admin panel, else the defaults).
// A failed read keeps the last known values.
func (s *Service) Pricing(ctx context.Context) Pricing {
	s.mu.RLock()
	if s.loaded && s.now().Sub(s.loadedAt) < s.ttl {
		p := s.pricing
		s.mu.RUnlock()
		return p
	}
	s.mu.RUnlock()
	fresh, err := s.readPricing(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.loaded {
			return s.pricing
		}
		return DefaultPricing()
	}
	s.pricing, s.loadedAt, s.loaded = fresh, s.now(), true
	return fresh
}

// UpdatePricing — admin panel: validates, stores and refreshes the cache.
func (s *Service) UpdatePricing(ctx context.Context, next Pricing) (Pricing, error) {
	if err := next.Validate(); err != nil {
		return Pricing{}, err
	}
	if err := s.store.SetSettings(ctx, next.settings()); err != nil {
		return Pricing{}, err
	}
	s.mu.Lock()
	s.pricing, s.loadedAt, s.loaded = next, s.now(), true
	s.mu.Unlock()
	return next, nil
}

func (s *Service) readPricing(ctx context.Context) (Pricing, error) {
	out := DefaultPricing()
	for _, f := range []struct {
		key    string
		target *int
		rng    Range
	}{
		{SettingCharsPerReply, &out.CharsPerReply, CharsPerReplyRange},
		{SettingFreeChars, &out.FreeChars, FreeCharsRange},
		{SettingMaxExtraReplies, &out.MaxExtraReplies, MaxExtraRepliesRange},
	} {
		raw, err := s.store.Setting(ctx, f.key)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return Pricing{}, err
		}
		// A row that does not parse or is out of range (hand-edited database)
		// keeps the default rather than silently changing what users pay.
		if v, convErr := strconv.Atoi(raw); convErr == nil && v >= f.rng.Min && v <= f.rng.Max {
			*f.target = v
		}
	}
	return out, nil
}
