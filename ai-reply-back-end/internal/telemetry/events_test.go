package telemetry

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

func testService() *Service {
	s := New(nil, config.Telemetry{Enabled: true}, "test-access-secret-that-is-long-enough-000",
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.clock = &traits.FixedClock{T: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	return s
}

func at(offset time.Duration) string {
	return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Add(offset).Format(time.RFC3339)
}

func TestIngestAcceptsOnlyAllowlistedEventsAndProperties(t *testing.T) {
	s := testService()
	res, err := s.Ingest(context.Background(), Batch{
		InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", SessionID: "a4b3c2d1-0000-4000-8000-000000000001",
		Events: []IncomingEvent{
			{Name: "app_opened", OccurredAt: at(-time.Minute), Properties: map[string]any{"cold_start": true}},
			{Name: "push_token_registration_failed", OccurredAt: at(0), Properties: map[string]any{
				"provider": "fcm", "error_code": "SERVICE_NOT_AVAILABLE", "request_id": "req_1234abcd"}},
			{Name: "keyboard_key_pressed", OccurredAt: at(0)},
			{Name: "login_failed", OccurredAt: at(0), Properties: map[string]any{"password": "hunter2"}},
			{Name: "login_failed", OccurredAt: at(0), Properties: map[string]any{"method": "sms"}},
			{Name: "app_backgrounded", OccurredAt: at(0), Properties: map[string]any{"foreground_seconds": 1.5}},
			{Name: "api_error", OccurredAt: at(0), Properties: map[string]any{"route": "/api/v1/ai/reply", "status": 502.0,
				"error_code": "AI_PROVIDER_UNAVAILABLE"}},
			{Name: "app_opened", OccurredAt: at(-8 * 24 * time.Hour)},
			{Name: "app_opened", OccurredAt: at(20 * time.Minute)},
			{Name: "app_opened", OccurredAt: "yesterday"},
			{Name: "unexpected_app_error", OccurredAt: at(0), Properties: map[string]any{"error_code": "has spaces"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 3 || res.Rejected != 8 {
		t.Fatalf("result = %+v", res)
	}
	reasons := map[string]int{}
	for _, r := range res.Rejections {
		reasons[r.Reason]++
	}
	for reason, want := range map[string]int{
		"unknown_event": 1, "unknown_property": 1, "invalid_property": 3, "time_out_of_range": 2, "invalid_time": 1,
	} {
		if reasons[reason] != want {
			t.Errorf("%s: %d, want %d (%v)", reason, reasons[reason], want, res.Rejections)
		}
	}
	queued := <-s.queue
	if len(queued.Events) != 3 || len(queued.Sessions) != 1 || queued.Sessions[0].EventCount != 3 {
		t.Fatalf("queued = %+v", queued)
	}
	failed := queued.Events[1]
	if failed.Outcome != "failure" || failed.ErrorCode != "SERVICE_NOT_AVAILABLE" || failed.RequestID != "req_1234abcd" {
		t.Fatalf("derived columns = %+v", failed)
	}
}

func TestIngestLimits(t *testing.T) {
	s := testService()
	events := make([]IncomingEvent, MaxEventsPerBatch+1)
	for i := range events {
		events[i] = IncomingEvent{Name: "app_opened", OccurredAt: at(0)}
	}
	if _, err := s.Ingest(context.Background(), Batch{InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Events: events}); err == nil {
		t.Fatal("51 events accepted")
	}
	if _, err := s.Ingest(context.Background(), Batch{InstallationID: "x", Events: events[:1]}); err == nil {
		t.Fatal("missing installation accepted")
	}
	props := map[string]any{}
	for i := 0; i < MaxPropertiesCount+1; i++ {
		props[strings.Repeat("p", i+1)] = true
	}
	res, _ := s.Ingest(context.Background(), Batch{InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e",
		Events: []IncomingEvent{{Name: "app_opened", OccurredAt: at(0), Properties: props}}})
	if res.Accepted != 0 {
		t.Fatalf("too many properties accepted: %+v", res)
	}
}

func TestDisabledTelemetryAcceptsNothing(t *testing.T) {
	s := testService()
	s.cfg.Enabled = false
	res, err := s.Ingest(context.Background(), Batch{Events: []IncomingEvent{{Name: "app_opened"}}})
	if err != nil || !res.Disabled || res.Accepted != 0 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestSubjectHashIsKeyedAndNormalised(t *testing.T) {
	a := testService()
	b := New(nil, config.Telemetry{}, "another-secret-that-is-long-enough-00000", slog.Default())
	h := a.SubjectHash("email", " User@Example.com ")
	if h == "" || h != a.SubjectHash("email", "user@example.com") || len(h) != 32 {
		t.Fatalf("hash = %q", h)
	}
	if h == b.SubjectHash("email", "user@example.com") {
		t.Fatal("the hash must depend on the server secret")
	}
	if h == a.SubjectHash("phone", "user@example.com") || a.SubjectHash("email", "") != "" {
		t.Fatal("kind separates namespaces; empty stays empty")
	}
}

func TestQueueNeverBlocks(t *testing.T) {
	s := testService()
	for i := 0; i < queueSize+10; i++ {
		s.RecordAPIError(domain.APIError{Route: "/api/v1/x", StatusCode: 500})
	}
	if s.dropped.Load() != 10 {
		t.Fatalf("dropped = %d", s.dropped.Load())
	}
}
