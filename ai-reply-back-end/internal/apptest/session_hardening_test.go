package apptest

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Шығу бүкіл тізбекті жабады: бұрын ротацияланған токенмен шыққанда да жаңа токен өледі,
// ал орнату тіркелгіден ажырайды.
func TestLogoutWithARotatedTokenEndsTheSession(t *testing.T) {
	h := newHarness(t)
	first := h.signIn("rotated-logout@example.com")
	h.mustRegister(installation(installID(91), "android", fcmToken(91)), first.access)
	second := h.refresh(first) // first.refresh is now rotated

	res := h.do(http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": first.refresh},
		map[string]string{"X-Installation-ID": installID(91)})
	if res.status != http.StatusOK || res.body["ok"] != true {
		t.Fatalf("logout with a rotated token: %d %s", res.status, res.raw)
	}
	if again := h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": second.refresh}, nil); again.status != http.StatusUnauthorized {
		t.Fatalf("the newest token of the family survived logout: %d", again.status)
	}
	if got := h.text(`SELECT user_id FROM app_installations WHERE installation_id = ?`, installID(91)); got != "" {
		t.Fatalf("still attached to %q", got)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM refresh_tokens WHERE user_id = ? AND revoked_at IS NULL`, first.userID); n != 0 {
		t.Fatalf("%d refresh tokens still open", n)
	}

	// Another session of the same account (another phone) is a different family and stays.
	h.clock.Advance(33 * time.Second) // the resend cooldown between codes
	phone := h.signIn("rotated-logout@example.com")
	h.clock.Advance(33 * time.Second)
	other := h.signIn("rotated-logout@example.com")
	if res := h.do(http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": phone.refresh}, nil); res.status != http.StatusOK {
		t.Fatalf("logout: %d", res.status)
	}
	if res := h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": other.refresh}, nil); res.status != http.StatusOK {
		t.Fatalf("another device was signed out: %d %s", res.status, res.raw)
	}
}

// Клиент жіберген device_id басқа тіркелгінің құрылғысы болса, ол жол өзгермейді.
func TestSignInCannotTakeOverAnotherAccountsDevice(t *testing.T) {
	h := newHarness(t)
	signIn := func(address, model string) response {
		h.t.Helper()
		mustStatus(t, h.do(http.MethodPost, "/api/v1/auth/request-otp", map[string]any{"identifier": address}, nil), http.StatusOK, "")
		res := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{"identifier": address, "code": "1111",
			"device": map[string]any{"device_id": "shared-device-0001", "platform": "android", "app_version": "1.0.0", "model": model}}, nil)
		mustStatus(t, res, http.StatusOK, "")
		return res
	}
	owner := signIn("device-owner@example.com", "Pixel 8")
	if owner.str("device_id") != "shared-device-0001" {
		t.Fatalf("the first account keeps the id it sent: %q", owner.str("device_id"))
	}
	intruder := signIn("device-intruder@example.com", "Forged")
	if got := intruder.str("device_id"); got == "" || got == "shared-device-0001" {
		t.Fatalf("the second account got the owner's device id: %q", got)
	}
	row := h.text(`SELECT user_id || '|' || model FROM devices WHERE id = 'shared-device-0001'`)
	if row != owner.str("user", "id")+"|Pixel 8" {
		t.Fatalf("the owner's device row changed: %q", row)
	}
	if got := h.text(`SELECT user_id FROM devices WHERE id = ?`, intruder.str("device_id")); got != intruder.str("user", "id") {
		t.Fatalf("the second account's device belongs to %q", got)
	}
	// The access token carries the device that really belongs to the account.
	devices := h.do(http.MethodGet, "/api/v1/me/devices", nil, h.auth(intruder.str("access_token")))
	if !strings.Contains(string(devices.raw), intruder.str("device_id")) || strings.Contains(string(devices.raw), "shared-device-0001") {
		t.Fatalf("devices: %s", devices.raw)
	}

	// The owner signing in again still updates its own row.
	h.clock.Advance(33 * time.Second)
	signIn("device-owner@example.com", "Pixel 9")
	if got := h.text(`SELECT model FROM devices WHERE id = 'shared-device-0001'`); got != "Pixel 9" {
		t.Fatalf("the owner's own update was lost: %q", got)
	}
}

// /healthz орта атын айтпайды.
func TestHealthzDoesNotNameTheEnvironment(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodGet, "/healthz", nil, nil)
	if res.status != http.StatusOK || len(res.body) != 1 || res.body["ok"] != true {
		t.Fatalf("healthz: %d %s", res.status, res.raw)
	}
	if strings.Contains(string(res.raw), "development") {
		t.Fatal("healthz names the environment")
	}
}

// Қауіпсіз X-Request-ID қайтарылады; басқасы жаңасымен ауыстырылады.
func TestRequestIDHeaderIsSanitised(t *testing.T) {
	h := newHarness(t)
	if res := h.do(http.MethodGet, "/healthz", nil, map[string]string{"X-Request-ID": "app-42-retry"}); res.header.Get("X-Request-ID") != "app-42-retry" {
		t.Fatalf("a safe id is echoed: %q", res.header.Get("X-Request-ID"))
	}
	res := h.do(http.MethodGet, "/healthz", nil, map[string]string{"X-Request-ID": `"><script>alert(1)</script>`})
	if got := res.header.Get("X-Request-ID"); got == "" || strings.ContainsAny(got, `<>"`) {
		t.Fatalf("an unsafe id was kept: %q", got)
	}
	if strings.Contains(h.logs.String(), "<script>") {
		t.Fatal("the unsafe id reached the log")
	}
}
