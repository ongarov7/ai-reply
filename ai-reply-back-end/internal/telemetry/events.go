package telemetry

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Шектер: бір сұраныс қанша оқиға, бір оқиға қанша қасиет әкеле алады.
const (
	MaxEventsPerBatch  = 50
	MaxPropertiesCount = 8
	maxPastAge         = 7 * 24 * time.Hour
	maxFutureSkew      = 10 * time.Minute
)

type kind int

const (
	kindBool kind = iota + 1
	kindInt
	kindCode  // short machine code: error codes, plan codes
	kindID    // opaque id: notification, delivery, request
	kindRoute // API route
	kindEnum
)

type prop struct {
	kind   kind
	values []string // kindEnum
}

var (
	methodProp   = prop{kind: kindEnum, values: []string{"email", "google", "apple"}}
	providerProp = prop{kind: kindEnum, values: []string{"fcm", "apns"}}
	codeProp     = prop{kind: kindCode}
	idProp       = prop{kind: kindID}
)

// schemas — рұқсат етілген оқиғалар және олардың қасиеттері. Басқасы қабылданбайды.
//
// Deliberately absent: key presses, keyboard usage, message or reply text,
// clipboard contents, anything typed by the person. AI reply outcomes are
// measured on the server (ai_usage_events), so the keyboard sends nothing.
var schemas = map[string]map[string]prop{
	"app_opened":                     {"cold_start": {kind: kindBool}},
	"app_backgrounded":               {"foreground_seconds": {kind: kindInt}},
	"login_success":                  {"method": methodProp},
	"login_failed":                   {"method": methodProp, "error_code": codeProp},
	"registration_success":           {"method": methodProp},
	"logout":                         {},
	"push_permission_requested":      {},
	"push_permission_granted":        {"status": {kind: kindEnum, values: []string{"authorized", "provisional", "ephemeral"}}},
	"push_permission_denied":         {},
	"push_token_registered":          {"provider": providerProp},
	"push_token_refreshed":           {"provider": providerProp},
	"push_token_registration_failed": {"provider": providerProp, "error_code": codeProp, "request_id": idProp},
	"notification_opened":            {"notification_id": idProp, "delivery_id": idProp, "type": codeProp},
	"payment_started":                {"plan_code": codeProp},
	"payment_success":                {"plan_code": codeProp},
	"payment_failed":                 {"plan_code": codeProp, "error_code": codeProp},
	"api_error": {"route": {kind: kindRoute}, "status": {kind: kindInt}, "error_code": codeProp,
		"request_id": idProp},
	"unexpected_app_error": {"error_code": codeProp, "where": codeProp},
}

// EventNames — рұқсат етілген атаулар (әкімші сүзгісіне).
func EventNames() []string {
	out := make([]string, 0, len(schemas))
	for name := range schemas {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var (
	codePattern  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	idPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	routePattern = regexp.MustCompile(`^[A-Za-z0-9_/{}.:-]{1,128}$`)
	sessPattern  = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
)

// IncomingEvent — қосымшадан келген бір оқиға.
type IncomingEvent struct {
	ID         string
	Name       string
	OccurredAt string // RFC 3339
	Properties map[string]any
}

// Batch — бір сұраныстың оқиғалары және оның контексі.
type Batch struct {
	InstallationID string
	SessionID      string
	UserID         string // from the access token, never from the body
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	DeviceModel    string
	RequestID      string
	Events         []IncomingEvent
}

// Rejection — неге оқиға қабылданбады (клиентке, мәнсіз).
type Rejection struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// IngestResult — нәтиже.
type IngestResult struct {
	Accepted   int         `json:"accepted"`
	Rejected   int         `json:"rejected"`
	Rejections []Rejection `json:"rejections,omitempty"`
	Disabled   bool        `json:"disabled,omitempty"`
}

// Ingest — оқиғаларды тексеріп, кезекке қояды. Жазу фонда жүреді.
func (s *Service) Ingest(ctx context.Context, b Batch) (IngestResult, error) {
	if !s.cfg.Enabled {
		return IngestResult{Disabled: true}, nil
	}
	if !sessPattern.MatchString(b.InstallationID) {
		return IngestResult{}, domain.InvalidField("installation_id", "required")
	}
	if b.SessionID != "" && !sessPattern.MatchString(b.SessionID) {
		return IngestResult{}, domain.InvalidField("session_id", "format")
	}
	if len(b.Events) == 0 || len(b.Events) > MaxEventsPerBatch {
		return IngestResult{}, domain.InvalidField("events", "1-50 events")
	}

	now := s.clock.Now()
	var (
		result  IngestResult
		batch   repository.TelemetryBatch
		session *domain.AppSession
	)
	for i, in := range b.Events {
		event, reason := s.validate(in, now)
		if reason != "" {
			result.Rejected++
			if len(result.Rejections) < 10 {
				result.Rejections = append(result.Rejections, Rejection{Index: i, Reason: reason})
			}
			continue
		}
		event.UserID, event.InstallationID, event.SessionID = b.UserID, b.InstallationID, b.SessionID
		event.Platform, event.AppVersion, event.AppBuild = b.Platform, b.AppVersion, b.AppBuild
		event.OSVersion, event.DeviceModel, event.ReceivedAt = b.OSVersion, b.DeviceModel, now
		if event.RequestID == "" {
			event.RequestID = b.RequestID
		}
		batch.Events = append(batch.Events, event)
		result.Accepted++

		if event.Name == "notification_opened" {
			if did, _ := event.Properties["delivery_id"].(string); did != "" {
				batch.Opens = append(batch.Opens, repository.OpenMark{
					DeliveryID: did, InstallationID: b.InstallationID, At: event.OccurredAt,
				})
			}
		}
		if b.SessionID != "" {
			if session == nil {
				session = &domain.AppSession{
					SessionID: b.SessionID, InstallationID: b.InstallationID, UserID: b.UserID,
					Platform: b.Platform, AppVersion: b.AppVersion, AppBuild: b.AppBuild,
					OSVersion: b.OSVersion, DeviceModel: b.DeviceModel,
					StartedAt: event.OccurredAt, LastActivityAt: event.OccurredAt,
				}
			}
			if event.OccurredAt.Before(session.StartedAt) {
				session.StartedAt = event.OccurredAt
			}
			if event.OccurredAt.After(session.LastActivityAt) {
				session.LastActivityAt = event.OccurredAt
			}
			if event.Name == "app_backgrounded" {
				at := event.OccurredAt
				session.EndedAt = &at
			}
			session.EventCount++
		}
	}
	if session != nil {
		batch.Sessions = append(batch.Sessions, *session)
	}
	if len(batch.Events) > 0 && !s.enqueue(batch) {
		// Analytics never fails the app: the batch is dropped and counted.
		result.Accepted, result.Rejected = 0, len(b.Events)
		result.Rejections = []Rejection{{Index: -1, Reason: "busy"}}
	}
	return result, nil
}

func (s *Service) validate(in IncomingEvent, now time.Time) (domain.AppEvent, string) {
	name := strings.TrimSpace(in.Name)
	schema, ok := schemas[name]
	if !ok {
		return domain.AppEvent{}, "unknown_event"
	}
	if in.ID != "" && !sessPattern.MatchString(in.ID) {
		return domain.AppEvent{}, "invalid_id"
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(in.OccurredAt))
	if err != nil {
		return domain.AppEvent{}, "invalid_time"
	}
	if at.Before(now.Add(-maxPastAge)) || at.After(now.Add(maxFutureSkew)) {
		return domain.AppEvent{}, "time_out_of_range"
	}
	if len(in.Properties) > MaxPropertiesCount {
		return domain.AppEvent{}, "too_many_properties"
	}
	props := make(map[string]any, len(in.Properties))
	for key, raw := range in.Properties {
		spec, ok := schema[key]
		if !ok {
			return domain.AppEvent{}, "unknown_property"
		}
		value, ok := coerce(spec, raw)
		if !ok {
			return domain.AppEvent{}, "invalid_property"
		}
		props[key] = value
	}
	event := domain.AppEvent{
		ClientEventID: in.ID, Name: name, Properties: props, OccurredAt: at.UTC(),
		Outcome: outcomeOf(name),
	}
	if code, _ := props["error_code"].(string); code != "" {
		event.ErrorCode = code
	}
	if rid, _ := props["request_id"].(string); rid != "" {
		event.RequestID = rid
	}
	return event, ""
}

func coerce(spec prop, raw any) (any, bool) {
	switch spec.kind {
	case kindBool:
		v, ok := raw.(bool)
		return v, ok
	case kindInt:
		f, ok := raw.(float64)
		if !ok || f < 0 || f > 1e9 || math.Trunc(f) != f {
			return nil, false
		}
		return int64(f), true
	case kindCode:
		v, ok := raw.(string)
		return v, ok && codePattern.MatchString(v)
	case kindID:
		v, ok := raw.(string)
		return v, ok && idPattern.MatchString(v)
	case kindRoute:
		v, ok := raw.(string)
		return v, ok && routePattern.MatchString(v)
	case kindEnum:
		v, ok := raw.(string)
		if !ok {
			return nil, false
		}
		for _, allowed := range spec.values {
			if v == allowed {
				return v, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func outcomeOf(name string) string {
	switch {
	case strings.HasSuffix(name, "_failed"), strings.HasSuffix(name, "_denied"),
		name == "api_error", name == "unexpected_app_error":
		return domain.OutcomeFailure
	case strings.HasSuffix(name, "_success"), strings.HasSuffix(name, "_registered"),
		strings.HasSuffix(name, "_granted"), strings.HasSuffix(name, "_refreshed"):
		return domain.OutcomeSuccess
	default:
		return ""
	}
}
