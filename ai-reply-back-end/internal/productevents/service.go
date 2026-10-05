package productevents

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// MaxEvents — the most events one request may carry.
const MaxEvents = 20

// Platforms — платформа коды: тек қолданбалар оқиға жібереді.
var Platforms = []string{"ios", "android"}

// appVersionPattern — a marketing version such as "1.4", "2.0.1" or
// "2.0.1 (57)": short dotted numbers, so neither a word nor a phone number can
// pass for one.
var appVersionPattern = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,4}){0,3}( \([0-9]{1,6}\))?$`)

// Batch — бір сұраныстағы оқиғалар.
type Batch struct {
	UserID     string
	Platform   string
	AppVersion string
	// Events — each element as the client sent it; Parse checks every one.
	Events []json.RawMessage
}

// Result — сақталған және каталог қабылдамаған оқиғалар саны.
type Result struct {
	Accepted int
	Rejected int
}

// InvalidBatchError — the request as a whole is unusable: 400 INVALID_REQUEST
// with the field. Individual bad events never cause it.
type InvalidBatchError struct {
	Field   string
	details map[string]any
}

func (e *InvalidBatchError) Error() string { return "invalid product event batch: " + e.Field }

// Unwrap — httpx.Translate maps it to INVALID_REQUEST.
func (e *InvalidBatchError) Unwrap() error { return domain.ErrInvalidRequest }

// Details — the field and, for the events list, its size limit.
func (e *InvalidBatchError) Details() map[string]any { return e.details }

// Service — оқиғаларды тексеріп, сақтайды.
type Service struct {
	repo  *repository.Store
	clock traits.Clock
	log   *slog.Logger
}

// New — оқиғалар қызметі.
func New(repo *repository.Store, log *slog.Logger) *Service {
	return &Service{repo: repo, clock: traits.SystemClock{}, log: log}
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// Record — каталогтан өткен оқиғаларды бір транзакцияда сақтайды, қалғанын
// санап қана қояды.
func (s *Service) Record(ctx context.Context, b Batch) (Result, error) {
	platform := strings.ToLower(strings.TrimSpace(b.Platform))
	if !slices.Contains(Platforms, platform) {
		return Result{}, &InvalidBatchError{Field: "platform", details: map[string]any{"field": "platform"}}
	}
	appVersion := strings.TrimSpace(b.AppVersion)
	if appVersion != "" && !appVersionPattern.MatchString(appVersion) {
		return Result{}, &InvalidBatchError{Field: "app_version", details: map[string]any{"field": "app_version"}}
	}
	if len(b.Events) == 0 || len(b.Events) > MaxEvents {
		return Result{}, &InvalidBatchError{Field: "events", details: map[string]any{"field": "events", "max_events": MaxEvents}}
	}

	rows := make([]repository.ProductEvent, 0, len(b.Events))
	for _, raw := range b.Events {
		event, err := Parse(raw)
		if err != nil {
			// Counted, never logged: a rejected event may hold anything.
			continue
		}
		props, err := json.Marshal(event.Props)
		if err != nil {
			// Ints, bools and strings always marshal; count it as rejected if not.
			continue
		}
		rows = append(rows, repository.ProductEvent{Name: event.Name, Props: string(props), ClientTime: event.ClientTime})
	}
	result := Result{Accepted: len(rows), Rejected: len(b.Events) - len(rows)}
	if result.Rejected > 0 {
		// A client and the catalog disagree: worth seeing, and safe to log, since
		// it is counts and enums only.
		s.log.Info("product_events_rejected", "user_id", b.UserID, "platform", platform,
			"app_version", appVersion, "accepted", result.Accepted, "rejected", result.Rejected)
	}
	if len(rows) == 0 {
		return result, nil
	}
	if err := s.repo.InsertProductEvents(ctx, b.UserID, platform, appVersion, rows, s.clock.Now()); err != nil {
		s.log.Error("product events insert failed", "user_id", b.UserID, "error", err.Error())
		return Result{}, err
	}
	return result, nil
}
