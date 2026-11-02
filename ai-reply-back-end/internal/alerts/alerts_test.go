package alerts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/logging"
)

type fakeTelegram struct {
	mu       sync.Mutex
	requests []map[string]any
	paths    []string
	status   []int
}

func (f *fakeTelegram) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.requests = append(f.requests, body)
		f.paths = append(f.paths, r.URL.Path)
		status := http.StatusOK
		if len(f.status) > 0 {
			status, f.status = f.status[0], f.status[1:]
		}
		f.mu.Unlock()
		w.WriteHeader(status)
		if status == http.StatusTooManyRequests {
			_, _ = w.Write([]byte(`{"ok":false,"parameters":{"retry_after":0}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func (f *fakeTelegram) wait(t *testing.T, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.requests) >= n {
			out := append([]map[string]any(nil), f.requests...)
			f.mu.Unlock()
			return out
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d telegram requests", n)
	return nil
}

func newTestNotifier(t *testing.T, srv *httptest.Server, tweak func(*Config)) *Notifier {
	t.Helper()
	cfg := Config{Enabled: true, BotToken: "123456:TESTTOKENTESTTOKENTESTTOKENTESTTOKEN", ChatID: "-100777",
		Service: "ai-reply", Environment: "test", Client: srv.Client()}
	if srv != nil {
		cfg.APIBase = srv.URL
	}
	if tweak != nil {
		tweak(&cfg)
	}
	return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNotifyDeliversScrubbedMessage(t *testing.T) {
	tg := &fakeTelegram{}
	srv := httptest.NewServer(tg.handler())
	defer srv.Close()
	n := newTestNotifier(t, srv, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Notify(Error, "subscription expiry sweep failed",
		"error", "database is locked", "email", "person@example.com", "order_id", "ord-1",
		"note", "contact person@example.com, card 4111111111111111, key sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	reqs := tg.wait(t, 1)
	if !strings.HasSuffix(tg.paths[0], "/bot"+n.cfg.BotToken+"/sendMessage") {
		t.Fatalf("unexpected path %s", tg.paths[0])
	}
	text, _ := reqs[0]["text"].(string)
	if reqs[0]["chat_id"] != "-100777" || reqs[0]["parse_mode"] != "HTML" {
		t.Fatalf("unexpected payload %v", reqs[0])
	}
	for _, want := range []string{"ERROR", "ai-reply", "test", "subscription expiry sweep failed", "database is locked", "ord-1", "[email]", "[number]", "[redacted]"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"person@example.com", "4111111111111111", "sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "email:"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("text leaks %q:\n%s", forbidden, text)
		}
	}
}

func TestDedupCountsRepeatsWithinWindow(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), nil)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		n.Notify(Error, "quota refund failed", "error", "database is locked", "request_id", string(rune('a'+i)))
	}
	if got := len(n.queue); got != 1 {
		t.Fatalf("expected one queued message within the window, got %d", got)
	}
	// A different error is a different alert.
	n.Notify(Error, "quota refund failed", "error", "disk I/O error")
	if got := len(n.queue); got != 2 {
		t.Fatalf("expected a second message for a different error, got %d", got)
	}
	now = now.Add(6 * time.Minute)
	n.Notify(Error, "quota refund failed", "error", "database is locked")
	if got := len(n.queue); got != 3 {
		t.Fatalf("expected the alert after the window, got %d", got)
	}
	<-n.queue
	<-n.queue
	m := <-n.queue
	if m.repeats != 4 {
		t.Fatalf("expected 4 counted repeats, got %d", m.repeats)
	}
	_, plain := n.render(m)
	if !strings.Contains(plain, "repeated 4 more time(s)") {
		t.Fatalf("repeat line missing:\n%s", plain)
	}
}

func TestRateLimitSuppressesAndSummarises(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), func(c *Config) { c.RatePerMinute = 2 })
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	for i := 0; i < 6; i++ {
		n.Notify(Warning, "openai request failed", "kind", string(rune('a'+i)))
	}
	if got := len(n.queue); got != 2 {
		t.Fatalf("expected 2 admitted messages, got %d", got)
	}
	n.flushSummary()
	if got := len(n.queue); got != 3 {
		t.Fatalf("expected a summary message, got %d queued", got)
	}
	<-n.queue
	<-n.queue
	summary := <-n.queue
	if summary.title != "alerts suppressed by rate limit" || summary.fields[0].value != "4" {
		t.Fatalf("unexpected summary %+v", summary)
	}
	n.flushSummary()
	if len(n.queue) != 0 {
		t.Fatal("summary must not repeat without new drops")
	}
	// Tokens refill with time.
	now = now.Add(time.Minute)
	n.Notify(Warning, "openai request failed", "kind", "z")
	if len(n.queue) != 1 {
		t.Fatal("expected the bucket to refill after a minute")
	}
}

func TestHandlerForwardsErrorAndCriticalRecords(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), nil)
	log := slog.New(n.Handler(slog.NewTextHandler(io.Discard, nil))).With("component", "worker")
	ctx := logging.WithRequestID(context.Background(), "req-42")
	log.Info("http", "status", 500)
	log.Warn("slow query", "duration_ms", 900)
	if len(n.queue) != 0 {
		t.Fatal("info and warn records must not alert")
	}
	log.Warn("BCC payment declined", logging.Alert(), "rc", "05")
	flaggedWarn := <-n.queue
	if flaggedWarn.severity != Warning || flaggedWarn.title != "BCC payment declined" {
		t.Fatalf("flagged warn must become WARNING, got %+v", flaggedWarn)
	}
	for _, f := range flaggedWarn.fields {
		if f.key == logging.AlertKey {
			t.Fatal("the alert flag itself must not be a field")
		}
	}
	log.ErrorContext(ctx, "generation commit failed", "error", "database is locked", "token", "secret-value")
	log.LogAttrs(ctx, logging.LevelCritical, "panic recovered", slog.String("path", "/api/v1/ai/reply"), slog.Any("panic", "nil map"), slog.String("stack", "goroutine 1 [running]..."))
	if got := len(n.queue); got != 2 {
		t.Fatalf("expected 2 alerts, got %d", got)
	}
	first := <-n.queue
	if first.severity != Error || first.title != "generation commit failed" {
		t.Fatalf("unexpected first alert %+v", first)
	}
	keys := map[string]string{}
	for _, f := range first.fields {
		keys[f.key] = f.value
	}
	if keys["component"] != "worker" || keys["error"] != "database is locked" || keys["request_id"] != "req-42" {
		t.Fatalf("fields missing: %v", keys)
	}
	if _, leaked := keys["token"]; leaked {
		t.Fatal("forbidden key must be dropped")
	}
	second := <-n.queue
	if second.severity != Critical {
		t.Fatalf("critical level must map to CRITICAL, got %s", second.severity)
	}
	for _, f := range second.fields {
		if f.key == "stack" {
			t.Fatal("stack traces must stay in the log")
		}
	}
}

func TestDisabledNotifierIsNoop(t *testing.T) {
	n := New(Config{Enabled: false}, nil)
	next := slog.NewTextHandler(io.Discard, nil)
	if n.Handler(next) != next {
		t.Fatal("disabled notifier must return the wrapped handler unchanged")
	}
	n.Notify(Critical, "ignored")
	done := make(chan struct{})
	go func() { n.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run must return immediately when disabled")
	}
	var nilNotifier *Notifier
	nilNotifier.Notify(Error, "nil receiver")
	if nilNotifier.Enabled() {
		t.Fatal("nil notifier is disabled")
	}
}

func TestFullQueueNeverBlocks(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), func(c *Config) { c.QueueSize = 4; c.RatePerMinute = 1000 })
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			n.Notify(Error, "distinct", "error", "e"+strconv.Itoa(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on a full queue")
	}
	if len(n.queue) != 4 {
		t.Fatalf("queue should hold its capacity, got %d", len(n.queue))
	}
	n.mu.Lock()
	dropped := n.suppressed
	n.mu.Unlock()
	if dropped != 496 {
		t.Fatalf("expected 496 drops, got %d", dropped)
	}
}

func TestDeliverRetriesOn429AndFallsBackToPlainOn400(t *testing.T) {
	tg := &fakeTelegram{status: []int{http.StatusTooManyRequests, http.StatusBadRequest, http.StatusOK}}
	srv := httptest.NewServer(tg.handler())
	defer srv.Close()
	n := newTestNotifier(t, srv, nil)
	n.deliver(context.Background(), message{severity: Critical, title: "panic recovered", at: time.Now(),
		fields: []field{{"path", "/api/v1/ai/reply"}}})
	reqs := tg.wait(t, 3)
	if _, html := reqs[1]["parse_mode"]; !html {
		t.Fatal("second attempt should still be HTML")
	}
	if _, html := reqs[2]["parse_mode"]; html {
		t.Fatal("after 400 the message must be sent as plain text")
	}
	if text, _ := reqs[2]["text"].(string); strings.Contains(text, "<b>") || !strings.Contains(text, "panic recovered") {
		t.Fatalf("plain fallback not plain:\n%s", text)
	}
}

func TestRejectedTokenDoesNotRetryAndRedactsLogs(t *testing.T) {
	tg := &fakeTelegram{status: []int{http.StatusUnauthorized}}
	srv := httptest.NewServer(tg.handler())
	defer srv.Close()
	var logs strings.Builder
	cfg := Config{Enabled: true, BotToken: "123456:TESTTOKENTESTTOKENTESTTOKENTESTTOKEN", ChatID: "1", APIBase: srv.URL, Client: srv.Client()}
	n := New(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	n.deliver(context.Background(), message{severity: Error, title: "x", at: time.Now()})
	if len(tg.wait(t, 1)) != 1 {
		t.Fatal("expected exactly one attempt")
	}
	if !strings.Contains(logs.String(), "telegram alert rejected") || strings.Contains(logs.String(), cfg.BotToken) {
		t.Fatalf("log must mention the rejection without the token:\n%s", logs.String())
	}
	// Unreachable endpoint: the error text carries the URL, the token must not survive.
	srv.Close()
	logs.Reset()
	_, _, err := n.send(context.Background(), "x", "")
	if err == nil {
		t.Fatal("expected a network error")
	}
	if strings.Contains(logs.String(), cfg.BotToken) {
		t.Fatalf("token leaked into the log:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "***") {
		t.Fatalf("expected the redacted url in the log:\n%s", logs.String())
	}
}

func TestWatchAlertsAfterTwoFailuresAndRecovers(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), nil)
	var mu sync.Mutex
	fail := true
	check := Check{Name: "database", Severity: Critical, Run: func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return context.DeadlineExceeded
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Watch(ctx, 5*time.Millisecond, check)
	waitFor := func(want int) message {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if len(n.queue) >= want {
				return <-n.queue
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("expected %d queued alerts", want)
		return message{}
	}
	first := waitFor(1)
	if first.severity != Critical || first.title != "database check failing" {
		t.Fatalf("unexpected alert %+v", first)
	}
	time.Sleep(30 * time.Millisecond)
	if len(n.queue) != 0 {
		t.Fatal("a failing check must alert once, not on every tick")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	recovered := waitFor(1)
	if recovered.severity != Recovered || recovered.title != "database check recovered" {
		t.Fatalf("unexpected recovery %+v", recovered)
	}
}

func TestDiskSpaceCheck(t *testing.T) {
	if err := DiskSpace(t.TempDir()+"/db.sqlite", 1)(context.Background()); err != nil {
		t.Fatalf("one byte must be free: %v", err)
	}
	if err := DiskSpace(t.TempDir()+"/db.sqlite", 1<<62)(context.Background()); err == nil {
		t.Fatal("expected a low-space error")
	}
}

func TestRepeatsInsideTheWindowAreReportedWhenItEnds(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), nil)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		n.Notify(Error, "BCC renewal charge unconfirmed", "error", "timeout")
	}
	<-n.queue // the first one goes at once
	n.flushSummary()
	if len(n.queue) != 0 {
		t.Fatal("repeats must wait for the end of the window")
	}
	now = now.Add(6 * time.Minute)
	n.flushSummary()
	if len(n.queue) != 1 {
		t.Fatalf("expected one repeat report, got %d", len(n.queue))
	}
	m := <-n.queue
	if m.repeats != 4 || m.title != "BCC renewal charge unconfirmed" {
		t.Fatalf("repeat report: %+v", m)
	}
	now = now.Add(6 * time.Minute)
	n.flushSummary()
	if len(n.queue) != 0 {
		t.Fatal("a reported burst must not be reported again")
	}
}

func TestHealthTransitionsAreNeverDeduplicated(t *testing.T) {
	n := newTestNotifier(t, httptest.NewUnstartedServer(nil), func(c *Config) { c.RatePerMinute = 1 })
	n.Notify(Warning, "filler", "kind", "uses the only token")
	for i := 0; i < 2; i++ {
		n.NotifyTransition(Critical, "database check failing", "check", "database", "error", "disk I/O error")
		n.NotifyTransition(Recovered, "database check recovered", "check", "database")
	}
	if got := len(n.queue); got != 5 {
		t.Fatalf("every transition must be queued, got %d", got)
	}
}
