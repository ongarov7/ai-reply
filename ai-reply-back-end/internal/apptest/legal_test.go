package apptest

import (
	"net/http"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/legal"
)

func TestLegalConfigAndConsentPersistence(t *testing.T) {
	h := newHarness(t)

	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if config.status != http.StatusOK {
		t.Fatalf("config: %d %s", config.status, config.raw)
	}
	if got := config.str("legal", "terms_version"); got != legal.TermsVersion {
		t.Fatalf("terms version = %q", got)
	}
	if got := config.str("legal", "privacy_url"); got != "http://localhost:8084/privacy" {
		t.Fatalf("privacy URL = %q", got)
	}
	if got := config.str("legal", "terms_url"); got != "http://localhost:8084/offer" {
		t.Fatalf("offer URL = %q", got)
	}

	unauthorized := h.do(http.MethodPost, "/api/v1/me/consents", map[string]any{
		"terms_version": legal.TermsVersion, "privacy_version": legal.PrivacyVersion,
		"locale": "ru", "platform": "ios",
	}, nil)
	if unauthorized.status != http.StatusUnauthorized {
		t.Fatalf("unauthorized consent accepted: %d", unauthorized.status)
	}

	session := h.signIn("+7 701 616 17 18")
	body := map[string]any{
		"terms_version": legal.TermsVersion, "privacy_version": legal.PrivacyVersion,
		"locale": "ru", "platform": "ios", "app_version": "1.2.3",
	}
	first := h.do(http.MethodPost, "/api/v1/me/consents", body, h.auth(session.access))
	if first.status != http.StatusOK {
		t.Fatalf("save consent: %d %s", first.status, first.raw)
	}
	if first.str("accepted_at") == "" {
		t.Fatal("accepted_at is missing")
	}

	second := h.do(http.MethodPost, "/api/v1/me/consents", body, h.auth(session.access))
	if second.status != http.StatusOK || second.str("accepted_at") != first.str("accepted_at") {
		t.Fatalf("consent is not idempotent: %d %s", second.status, second.raw)
	}

	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if got := me.str("legal_consent", "privacy_version"); got != legal.PrivacyVersion {
		t.Fatalf("privacy version from /me = %q", got)
	}
	if got := me.str("legal_consent", "platform"); got != "ios" {
		t.Fatalf("platform from /me = %q", got)
	}
}

func TestLegalConsentRejectsUnknownVersions(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 626 27 28")

	res := h.do(http.MethodPost, "/api/v1/me/consents", map[string]any{
		"terms_version": "old", "privacy_version": legal.PrivacyVersion,
		"locale": "kk", "platform": "android",
	}, h.auth(session.access))
	if res.status != http.StatusBadRequest {
		t.Fatalf("unknown version accepted: %d %s", res.status, res.raw)
	}
}

// /api/v1/config: қолдау, жою беті, AI провайдері және жаңа мүмкіндіктер.
func TestConfigAnnouncesLegalLinksAndFeatures(t *testing.T) {
	h := newHarness(t, withEnv("CONTACT_EMAIL", "support@ai-reply.kz"))
	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	for path, want := range map[[2]string]string{
		{"legal", "contact_email"}:        "support@ai-reply.kz",
		{"legal", "support_url"}:          "http://localhost:8084/support",
		{"legal", "account_deletion_url"}: "http://localhost:8084/account/delete",
		{"legal", "ai_provider"}:          "OpenAI",
		{"legal", "terms_version"}:        "2026-10-08",
		{"legal", "privacy_version"}:      "2026-10-08",
		{"payment_mode", ""}:              "demo",
	} {
		keys := []string{path[0]}
		if path[1] != "" {
			keys = append(keys, path[1])
		}
		if got := config.str(keys...); got != want {
			t.Errorf("%v = %q, want %q", keys, got, want)
		}
	}
	features, _ := config.body["features"].(map[string]any)
	if features["ai_reports"] != true || features["account_deletion"] != true {
		t.Fatalf("features = %v", features)
	}
}
