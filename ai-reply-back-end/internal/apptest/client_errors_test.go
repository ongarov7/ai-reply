package apptest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The apps report crashes and critical errors here; the server turns them into log records
// at the alerting levels (crash CRITICAL, purchase ERROR, others WARNING with the alert flag).
func TestClientErrorReportsReachTheLogAtAlertLevels(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("client-errors@example.com")
	crash := map[string]any{"platform": "ios", "app_version": "1.2.0 (16)", "os_version": "18.1", "kind": "crash",
		"code": "EXC_BAD_ACCESS:SIGSEGV", "detail": "KeyboardViewController.swift:212 contact me@example.com"}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/client-errors", crash, h.auth(s.access)), http.StatusAccepted, "")
	anonymous := map[string]any{"platform": "android", "kind": "purchase", "code": "PURCHASE_INVALID"}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/client-errors", anonymous, nil), http.StatusAccepted, "")
	other := map[string]any{"platform": "macos", "kind": "api", "code": "AI_TIMEOUT", "count": 3}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/client-errors", other, nil), http.StatusAccepted, "")

	logs := h.logs.String()
	if strings.Contains(logs, "me@example.com") {
		t.Error("an e-mail address in the report reached the log")
	}
	anonymousCrash := map[string]any{"platform": "windows", "kind": "crash", "code": "STATUS_ACCESS_VIOLATION",
		"detail": "update at https://evil.example/fix now"}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/client-errors", anonymousCrash, nil), http.StatusAccepted, "")

	logs = h.logs.String()
	if strings.Contains(logs, "evil.example") {
		t.Error("a link in an app report reached the log")
	}
	for _, want := range []string{`"level":"ERROR+4","msg":"app crash reported"`, `"level":"WARN","msg":"app purchase error reported"`,
		`"level":"ERROR","msg":"app crash reported"`, `"source":"app"`,
		`"level":"WARN","msg":"app error reported"`, `"alert":true`, `"code":"EXC_BAD_ACCESS:SIGSEGV"`, `"user_id":"` + s.userID + `"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %s", want)
		}
	}

	for name, bad := range map[string]map[string]any{
		"platform":      {"platform": "symbian", "kind": "crash", "code": "X"},
		"kind":          {"platform": "ios", "kind": "panic", "code": "X"},
		"code":          {"platform": "ios", "kind": "crash", "code": "has spaces"},
		"unknown field": {"platform": "ios", "kind": "crash", "code": "X", "clipboard": "secret"},
		"count":         {"platform": "ios", "kind": "crash", "code": "X", "count": 100000},
	} {
		if res := h.do(http.MethodPost, "/api/v1/client-errors", bad, nil); res.status != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d %s", name, res.status, res.raw)
		}
	}
}

func TestClientErrorReportsAreRateLimitedPerSender(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("client-errors-flood@example.com")
	limited := 0
	for i := 0; i < 25; i++ {
		res := h.do(http.MethodPost, "/api/v1/client-errors",
			map[string]any{"platform": "android", "kind": "crash", "code": fmt.Sprintf("E%d", i)}, h.auth(s.access))
		if res.status == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 5 {
		t.Fatalf("expected 5 refused reports out of 25, got %d", limited)
	}
}
