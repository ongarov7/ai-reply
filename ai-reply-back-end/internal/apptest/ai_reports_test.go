package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// report — AI жауабына шағым (POST /api/v1/ai/reports).
func (h *harness) report(access string, body map[string]any) response {
	return h.do(http.MethodPost, "/api/v1/ai/reports", body, h.auth(access))
}

const reportedText = "ШАҒЫМ МӘТІНІ: сіз ақымақсыз, REPORT-55120"

// Шағым сақталады, әкімшілер оны мәтінімен көреді және шешеді; журналда мәтін жоқ.
func TestAIReportReachesTheAdmins(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("reporter@example.com")

	features := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)
	if features["ai_reports"] != true || features["account_deletion"] != true {
		t.Fatalf("features = %v", features)
	}

	created := h.report(s.access, map[string]any{
		"mode": "reply", "reason": "offensive", "comment": "Дөрекі жауап", "text": reportedText,
		"platform": "ios", "app_version": "1.4.0",
	})
	if created.status != http.StatusCreated || created.str("id") == "" || len(created.body) != 1 {
		t.Fatalf("report: %d %s", created.status, created.raw)
	}
	id := created.str("id")
	// Without the text, as when the person switched the toggle off.
	mustStatus(t, h.report(s.access, map[string]any{"mode": "compose", "reason": "wrong_language"}), http.StatusCreated, "")

	if row := h.text(`SELECT mode || '|' || reason || '|' || comment || '|' || text || '|' || platform || '|' || status
		FROM ai_reports WHERE id = ?`, id); row != "reply|offensive|Дөрекі жауап|"+reportedText+"|ios|open" {
		t.Fatalf("stored = %q", row)
	}
	for _, needle := range []string{reportedText, "REPORT-55120", "Дөрекі жауап"} {
		if strings.Contains(h.logs.String(), needle) {
			t.Fatalf("logs contain %q", needle)
		}
	}

	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	list := h.do(http.MethodGet, "/api/v1/admin/reports?status=open", nil, headers)
	reports, _ := list.body["reports"].([]any)
	if list.status != http.StatusOK || list.num("total") != 2 || len(reports) != 2 {
		t.Fatalf("admin list: %d %s", list.status, list.raw)
	}
	// Newest first: both were sent at the same moment, so check by id instead of order.
	var first map[string]any
	for _, item := range reports {
		if row := item.(map[string]any); row["id"] == id {
			first = row
		}
	}
	if first == nil || first["text"] != reportedText || first["reason"] != "offensive" || first["user_id"] != s.userID ||
		first["identifier"] == "reporter@example.com" || !strings.Contains(first["identifier"].(string), "*") {
		t.Fatalf("report row = %v", first)
	}

	// Resolving needs the CSRF header, is audited once, and is harmless to repeat.
	noCSRF := h.do(http.MethodPost, "/api/v1/admin/reports/"+id+"/resolve", map[string]any{},
		map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie})
	if noCSRF.status != http.StatusForbidden {
		t.Fatalf("resolve without CSRF: %d", noCSRF.status)
	}
	resolved := h.do(http.MethodPost, "/api/v1/admin/reports/"+id+"/resolve", map[string]any{}, headers)
	again := h.do(http.MethodPost, "/api/v1/admin/reports/"+id+"/resolve", map[string]any{}, headers)
	if resolved.status != http.StatusOK || resolved.body["changed"] != true || again.status != http.StatusOK || again.body["changed"] != false {
		t.Fatalf("resolve: %s / %s", resolved.raw, again.raw)
	}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/admin/reports/no-such-report/resolve", map[string]any{}, headers),
		http.StatusNotFound, "NOT_FOUND")
	entries, _, _ := h.admin.AuditLog(context.Background(), traits.NewPage(50, 0))
	audited := 0
	for _, e := range entries {
		if e.Action == "report.resolve" && e.EntityID == id {
			audited++
		}
	}
	if audited != 1 {
		t.Fatalf("report.resolve audited %d times", audited)
	}

	open := h.do(http.MethodGet, "/api/v1/admin/reports?status=open", nil, headers)
	done := h.do(http.MethodGet, "/api/v1/admin/reports?status=resolved", nil, headers)
	all := h.do(http.MethodGet, "/api/v1/admin/reports", nil, headers)
	if open.num("total") != 1 || done.num("total") != 1 || all.num("total") != 2 {
		t.Fatalf("filters: open %s / resolved %s / all %s", open.raw, done.raw, all.raw)
	}
	row := done.body["reports"].([]any)[0].(map[string]any)
	if row["status"] != "resolved" || row["resolved_by"] != adminEmail || row["resolved_at"] == "" {
		t.Fatalf("resolved row = %v", row)
	}
	mustStatus(t, h.do(http.MethodGet, "/api/v1/admin/reports?status=spam", nil, headers), http.StatusBadRequest, "INVALID_REQUEST")
	if res := h.do(http.MethodGet, "/api/v1/admin/reports", nil, nil); res.status != http.StatusUnauthorized {
		t.Fatalf("reports without a session: %d", res.status)
	}
}

// Себеп, режим және өлшем серверде тексеріледі; ұзын мәтін қысқартылмай, қабылданбайды.
func TestAIReportValidation(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("report-rules@example.com")

	if res := h.do(http.MethodPost, "/api/v1/ai/reports", map[string]any{"mode": "reply", "reason": "other"}, nil); res.status != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", res.status)
	}
	for field, body := range map[string]map[string]any{
		"reason":   {"mode": "reply", "reason": "spam"},
		"mode":     {"mode": "polish", "reason": "other"},
		"comment":  {"mode": "reply", "reason": "other", "comment": strings.Repeat("ж", 501)},
		"text":     {"mode": "reply", "reason": "other", "text": strings.Repeat("ж", 2001)},
		"platform": {"mode": "reply", "reason": "other", "platform": "web"},
	} {
		res := h.report(s.access, body)
		if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_REQUEST" || res.str("error", "details", "field") != field {
			t.Errorf("%s: %d %s", field, res.status, res.raw)
		}
	}
	mustStatus(t, h.report(s.access, map[string]any{"mode": "reply", "reason": "other", "source_text": "x"}),
		http.StatusBadRequest, "INVALID_REQUEST")
	if n := h.scalar(`SELECT COUNT(*) FROM ai_reports`); n != 0 {
		t.Fatalf("%d invalid reports stored", n)
	}

	// The limits themselves are accepted, and every reason the apps offer.
	for _, reason := range []string{"offensive", "harmful", "false_info", "wrong_language", "other"} {
		mustStatus(t, h.report(s.access, map[string]any{
			"mode": "compose", "reason": reason, "comment": strings.Repeat("ж", 500), "text": strings.Repeat("ж", 2000),
			"platform": "android", "app_version": "2.0.1 (57)",
		}), http.StatusCreated, "")
	}

	// A report needs no AI consent: nothing in it goes to OpenAI.
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me/consents", nil, h.auth(s.access)), http.StatusOK, "")
	mustStatus(t, h.report(s.access, map[string]any{"mode": "reply", "reason": "harmful"}), http.StatusCreated, "")
}

func TestAIReportsHaveTheirOwnRateLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_AI_REPORTS_PER_HOUR", "2"))
	s := h.signIn("report-flood@example.com")
	other := h.signIn("report-calm@example.com")
	body := map[string]any{"mode": "reply", "reason": "other"}

	mustStatus(t, h.report(s.access, body), http.StatusCreated, "")
	mustStatus(t, h.report(s.access, body), http.StatusCreated, "")
	mustStatus(t, h.report(s.access, body), http.StatusTooManyRequests, "RATE_LIMITED")
	// Per account, and replies are not affected.
	mustStatus(t, h.report(other.access, body), http.StatusCreated, "")
	mustStatus(t, h.generate(s.access, "Сәлем!"), http.StatusOK, "")
}
