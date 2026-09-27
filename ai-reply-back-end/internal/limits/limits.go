// Package limits — AI сұранысының операциялық шектеулерінің жалғыз көзі.
//
// Source of truth, in this order:
//
//  1. system_settings rows written from the admin panel;
//  2. the environment (LIMIT_SOURCE_TEXT_CHARS, LIMIT_INSTRUCTION_CHARS,
//     OPENAI_MAX_OUTPUT_TOKENS) — the deployment default;
//  3. the code defaults below.
//
// Characters and tokens are deliberately separate concepts. Character limits
// are what a person sees and what the apps validate against before a request
// is ever made; max_output_tokens is a cost control on the provider call; how
// many generations a user gets per day lives on their plan
// (plans.daily_message_limit) and is not a setting here at all.
package limits

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Setting keys. They double as the JSON field names the clients already read
// from /api/v1/config, so one name means one thing everywhere.
const (
	KeySourceChars      = "max_source_characters"
	KeyInstructionChars = "max_instruction_length"
	KeyMaxOutputTokens  = "max_output_tokens"
)

// Code defaults, used only when neither the admin panel nor the environment
// says otherwise.
const (
	DefaultSourceChars      = 400
	DefaultInstructionChars = 400
	DefaultMaxOutputTokens  = 180
)

// Range — an inclusive range an administrator may choose from.
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// The ranges are wide enough for any real product decision and narrow enough
// that a typo in the admin panel cannot turn the keyboard into an essay
// generator or starve the model of room to answer.
var (
	SourceRange      = Range{Min: 50, Max: 2000}
	InstructionRange = Range{Min: 50, Max: 1000}
	OutputTokenRange = Range{Min: 32, Max: 1024}
)

// Limits — the values in effect.
type Limits struct {
	SourceChars      int `json:"max_source_characters"`
	InstructionChars int `json:"max_instruction_length"`
	MaxOutputTokens  int `json:"max_output_tokens"`
}

// FieldError — which value was out of range, for the admin panel to show.
type FieldError struct {
	Field string
	Range Range
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s must be between %d and %d", e.Field, e.Range.Min, e.Range.Max)
}

// Unwrap lets the HTTP layer treat it as an ordinary invalid request.
func (e *FieldError) Unwrap() error { return domain.ErrInvalidRequest }

// Validate — every value inside its range.
func (l Limits) Validate() error {
	checks := []struct {
		field string
		value int
		rng   Range
	}{
		{KeySourceChars, l.SourceChars, SourceRange},
		{KeyInstructionChars, l.InstructionChars, InstructionRange},
		{KeyMaxOutputTokens, l.MaxOutputTokens, OutputTokenRange},
	}
	for _, c := range checks {
		if c.value < c.rng.Min || c.value > c.rng.Max {
			return &FieldError{Field: c.field, Range: c.rng}
		}
	}
	return nil
}

// Store — persistence. repository.Store satisfies it.
type Store interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSettings(ctx context.Context, values map[string]string) error
}

// Service — reads the effective limits and lets the admin panel change them.
type Service struct {
	store    Store
	defaults Limits
	ttl      time.Duration
	now      func() time.Time

	mu       sync.RWMutex
	current  Limits
	loadedAt time.Time
	loaded   bool
}

// New — defaults are the deployment's own values (environment or code).
// Anything invalid in them is replaced by the code default rather than
// trusted, so a bad environment variable cannot disable the limit.
func New(store Store, defaults Limits) *Service {
	return &Service{store: store, defaults: sanitize(defaults), ttl: 5 * time.Second, now: time.Now}
}

// WithTTL — for tests that need every read to hit the store.
func (s *Service) WithTTL(ttl time.Duration) *Service { s.ttl = ttl; return s }

// Defaults — what applies when the admin panel has not overridden a value.
func (s *Service) Defaults() Limits { return s.defaults }

// Current — the hot path, called on every AI request.
//
// Cached for a few seconds so a busy server does not read three settings
// rows per request, and short enough that a change in the admin panel reaches
// every instance almost at once. A failed read keeps the last known values:
// a database hiccup must never change what users are allowed to send.
func (s *Service) Current(ctx context.Context) Limits {
	s.mu.RLock()
	if s.loaded && s.now().Sub(s.loadedAt) < s.ttl {
		current := s.current
		s.mu.RUnlock()
		return current
	}
	s.mu.RUnlock()

	fresh, err := s.read(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.loaded {
			return s.current
		}
		return s.defaults
	}
	s.current, s.loadedAt, s.loaded = fresh, s.now(), true
	return fresh
}

// Overridden — which keys currently come from the admin panel.
func (s *Service) Overridden(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for _, key := range []string{KeySourceChars, KeyInstructionChars, KeyMaxOutputTokens} {
		_, err := s.store.Setting(ctx, key)
		switch {
		case err == nil:
			out[key] = true
		case errors.Is(err, domain.ErrNotFound):
			out[key] = false
		default:
			return nil, err
		}
	}
	return out, nil
}

// Update — validates, persists atomically and refreshes the cache, so the
// administrator's next page load and the next AI request both see it.
func (s *Service) Update(ctx context.Context, next Limits) (Limits, error) {
	if err := next.Validate(); err != nil {
		return Limits{}, err
	}
	if err := s.store.SetSettings(ctx, map[string]string{
		KeySourceChars:      strconv.Itoa(next.SourceChars),
		KeyInstructionChars: strconv.Itoa(next.InstructionChars),
		KeyMaxOutputTokens:  strconv.Itoa(next.MaxOutputTokens),
	}); err != nil {
		return Limits{}, err
	}
	s.mu.Lock()
	s.current, s.loadedAt, s.loaded = next, s.now(), true
	s.mu.Unlock()
	return next, nil
}

func (s *Service) read(ctx context.Context) (Limits, error) {
	out := s.defaults
	fields := []struct {
		key    string
		target *int
		rng    Range
	}{
		{KeySourceChars, &out.SourceChars, SourceRange},
		{KeyInstructionChars, &out.InstructionChars, InstructionRange},
		{KeyMaxOutputTokens, &out.MaxOutputTokens, OutputTokenRange},
	}
	for _, f := range fields {
		raw, err := s.store.Setting(ctx, f.key)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return Limits{}, err
		}
		// A row that does not parse, or parses outside the range, is ignored
		// in favour of the default. It can only get there by hand-editing the
		// database, and a limit must never silently become zero.
		if v, convErr := strconv.Atoi(raw); convErr == nil && v >= f.rng.Min && v <= f.rng.Max {
			*f.target = v
		}
	}
	return out, nil
}

func sanitize(l Limits) Limits {
	if l.SourceChars < SourceRange.Min || l.SourceChars > SourceRange.Max {
		l.SourceChars = DefaultSourceChars
	}
	if l.InstructionChars < InstructionRange.Min || l.InstructionChars > InstructionRange.Max {
		l.InstructionChars = DefaultInstructionChars
	}
	if l.MaxOutputTokens < OutputTokenRange.Min || l.MaxOutputTokens > OutputTokenRange.Max {
		l.MaxOutputTokens = DefaultMaxOutputTokens
	}
	return l
}
