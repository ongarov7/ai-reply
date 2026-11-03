package apptest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/audio/audiotest"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func strictProDevices() harnessOption {
	return func(env map[string]string) {
		env["PRO_DEVICE_LIMIT_ENABLED"] = "true"
		env["PRO_DEVICE_LIMIT_SHADOW_MODE"] = "false"
		env["PRO_DEVICE_LIMIT_CLIENTS_READY"] = "true"
		env["PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED"] = "false"
	}
}

// Models the optional compatible client contract, without changing any native
// client: installation identity is signed in at login and registered against
// the same authenticated device. Empty identity models existing released apps.
func (h *harness) signInProDevice(address, deviceID, platform, installationID string) (session, response) {
	h.t.Helper()
	h.clock.Advance(33 * time.Second)
	mustStatus(h.t, h.do(http.MethodPost, "/api/v1/auth/request-otp",
		map[string]any{"identifier": address, "locale": "en"}, nil), http.StatusOK, "")
	device := map[string]any{"device_id": deviceID, "platform": platform, "app_version": "1.0.0", "locale": "en"}
	if installationID != "" {
		device["installation_id"] = installationID
	}
	res := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"identifier": address, "code": "1111", "device": device,
	}, nil)
	mustStatus(h.t, res, http.StatusOK, "")
	s := session{access: res.str("access_token"), refresh: res.str("refresh_token"), userID: res.str("user", "id")}
	h.consent(s.access)
	if installationID != "" {
		h.mustRegister(installation(installationID, platform, ""), s.access)
	}
	return s, res
}

func (h *harness) proDeviceAuthorization(access, category string) string {
	h.t.Helper()
	res := h.do(http.MethodGet, "/api/v1/me/pro-devices", nil, h.auth(access))
	mustStatus(h.t, res, http.StatusOK, "")
	devices, _ := res.body["devices"].([]any)
	for _, value := range devices {
		d, _ := value.(map[string]any)
		if d["device_category"] == category && d["revoked_at"] == nil {
			id, _ := d["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	h.t.Fatalf("no active %s authorization: %s", category, res.raw)
	return ""
}

func TestProDeviceCategoryPolicyThroughAPI(t *testing.T) {
	for _, tc := range []struct {
		first, second string
		blocked       bool
	}{
		{"ios", "android", true}, {"android", "ios", true},
		{"ios", "ios", true}, {"ipados", "android", true},
		{"macos", "windows", true}, {"windows", "macos", true},
		{"macos", "macos", true}, {"ios", "macos", false},
		{"android", "windows", false}, {"windows", "ios", false},
	} {
		t.Run(tc.first+"_then_"+tc.second, func(t *testing.T) {
			h := newHarness(t, approvedEntitlements(), strictProDevices())
			first, _ := h.signInProDevice("pro-category@example.com", "first-device-0001", tc.first, installID(801))
			h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
			mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
			second, _ := h.signInProDevice("pro-category@example.com", "second-device-0002", tc.second, installID(802))
			res := h.generate(second.access, "Сәлем")
			if tc.blocked {
				mustStatus(t, res, http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
				category := "mobile"
				if tc.second == "macos" || tc.second == "windows" {
					category = "desktop"
				}
				if res.str("error", "details", "device_category") != category || res.num("error", "details", "allowed_devices") != 1 {
					t.Fatalf("limit details: %s", res.raw)
				}
			} else {
				mustStatus(t, res, http.StatusOK, "")
			}
			// The blocked login/request never displaces the original installation.
			mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
			mustStatus(t, h.do(http.MethodGet, "/api/v1/me", nil, h.auth(second.access)), http.StatusOK, "")
		})
	}
}

func TestProDeviceDeniedBeforeEveryAIProviderAndCreditReservation(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices(), withWelcomeBonus(10))
	first, _ := h.signInProDevice("pro-endpoints@example.com", "endpoint-device-0001", "ios", installID(811))
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	h.addGrant(first.userID, domain.GrantPack, 30, h.clock.Now(), nil, h.clock.Now())
	allowed := h.generate(first.access, "Сәлем")
	mustStatus(t, allowed, http.StatusOK, "")
	if allowed.str("usage", "bucket") != "unlimited" {
		t.Fatalf("Pro lost unlimited entitlement: %s", allowed.raw)
	}
	second, _ := h.signInProDevice("pro-endpoints@example.com", "endpoint-device-0002", "android", installID(812))
	before := h.provider.calls
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.compose(second.access, map[string]any{"instruction": "Әріптесімді құттықта", "language": "kk", "platform": "macos"}), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.polish(second.access, map[string]any{"text": polishNote, "input_language": "ru", "platform": "windows"}), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.transcribe(second.access, audiotest.M4A(2*time.Second, nil), voiceFields()), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	if h.provider.calls != before || len(h.speech.requests()) != 0 {
		t.Fatal("a denied installation reached an AI provider")
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(second.access))
	if usage.num("purchased_credits") != 30 || usage.num("welcome", "remaining") != 10 || usage.body["unlimited"] != true {
		t.Fatalf("denial changed entitlement or consumed balances: %s", usage.raw)
	}
	if len(h.push.messages()) != 0 || len(h.mail.messages()) != 0 {
		t.Fatal("authorization performed synchronous notification delivery")
	}
}

func TestProDeviceShadowKeepsReleasedClientsWorkingWithoutClaimingSlots(t *testing.T) {
	h := newHarness(t, approvedEntitlements())
	first, _ := h.signInProDevice("pro-shadow@example.com", "shadow-device-0001", "ios", "")
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	second, _ := h.signInProDevice("pro-shadow@example.com", "shadow-device-0002", "android", "")
	for _, s := range []session{first, second, h.refresh(first)} {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	if h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, first.userID) != 0 {
		t.Fatal("shadow mode selected an authorization winner")
	}
	if h.scalar(`SELECT COUNT(*) FROM pro_device_notification_jobs WHERE user_id=?`, first.userID) != 0 {
		t.Fatal("shadow mode claimed that an installation had been blocked")
	}
}

func TestProDeviceStrictMissingIdentityIsExplicitAndNonProUnaffected(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices(), withWelcomeBonus(10))
	first, _ := h.signInProDevice("pro-missing@example.com", "missing-device-0001", "ios", "")
	// Old client account functionality and its Free allowance remain available.
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	admin := h.signInAdmin()
	h.assignPlan(admin, first.userID, "base_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	h.assignPlan(admin, first.userID, "pro_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	// A push registration cannot silently add a signed installation claim to
	// an access token issued to an old client without that identity.
	h.mustRegister(installation(installID(821), "ios", ""), first.access)
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	mustStatus(t, h.do(http.MethodGet, "/api/v1/me", nil, h.auth(first.access)), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, first.userID) != 0 {
		t.Fatal("missing identity reserved a slot")
	}
}

func TestProDeviceSameInstallationRefreshKeyboardAndAccountSwitch(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices())
	first, firstResponse := h.signInProDevice("pro-return@example.com", "return-device-0001", "ios", installID(831))
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	authorization := h.proDeviceAuthorization(first.access, "mobile")
	refreshed := h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": first.refresh,
		"device":        map[string]any{"device_id": "forged-other-device", "installation_id": installID(832), "platform": "windows"},
	}, nil)
	mustStatus(t, refreshed, http.StatusOK, "")
	if refreshed.str("device_id") != firstResponse.str("device_id") {
		t.Fatal("refresh changed the session's canonical device")
	}
	// Host and keyboard share this signed session; no installation request
	// field or push token is present on either generation.
	for i := 0; i < 2; i++ {
		mustStatus(t, h.generate(refreshed.str("access_token"), "Сәлем"), http.StatusOK, "")
	}
	other, otherResponse := h.signInProDevice("pro-return-other@example.com", firstResponse.str("device_id"), "ios", installID(831))
	mustStatus(t, h.do(http.MethodGet, "/api/v1/me", nil, h.auth(other.access)), http.StatusOK, "")
	returned, returnedResponse := h.signInProDevice("pro-return@example.com", otherResponse.str("device_id"), "ios", installID(831))
	if returnedResponse.str("device_id") != firstResponse.str("device_id") {
		t.Fatalf("account switching changed canonical device: %s", returnedResponse.raw)
	}
	mustStatus(t, h.generate(returned.access, "Сәлем"), http.StatusOK, "")
	if got := h.proDeviceAuthorization(returned.access, "mobile"); got != authorization {
		t.Fatal("returning installation acquired a different authorization")
	}
	if h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, first.userID) != 1 {
		t.Fatal("same installation consumed multiple authorization records")
	}
}

func TestProDeviceRecoveryCannotBeStartedByBlockedDevice(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices())
	mobile, mobileResponse := h.signInProDevice("pro-recovery@example.com", "recovery-mobile-0001", "ios", installID(841))
	h.assignPlan(h.signInAdmin(), mobile.userID, "pro_monthly", "")
	mustStatus(t, h.generate(mobile.access, "Сәлем"), http.StatusOK, "")
	id := h.proDeviceAuthorization(mobile.access, "mobile")
	blocked, _ := h.signInProDevice("pro-recovery@example.com", "recovery-mobile-0002", "android", installID(842))
	mustStatus(t, h.generate(blocked.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	denied := h.do(http.MethodDelete, "/api/v1/me/pro-devices/"+id, nil, h.auth(blocked.access))
	if denied.status != http.StatusForbidden {
		t.Fatalf("blocked device could revoke original: %d %s", denied.status, denied.raw)
	}
	// Existing device deletion must not be an alternate recovery bypass.
	denied = h.do(http.MethodDelete, "/api/v1/devices/"+mobileResponse.str("device_id"), nil, h.auth(blocked.access))
	if denied.status != http.StatusForbidden {
		t.Fatalf("legacy device endpoint bypassed recovery authorization: %d %s", denied.status, denied.raw)
	}
	mustStatus(t, h.generate(mobile.access, "Сәлем"), http.StatusOK, "")
	desktop, _ := h.signInProDevice("pro-recovery@example.com", "recovery-desktop-0001", "macos", installID(843))
	mustStatus(t, h.generate(desktop.access, "Сәлем"), http.StatusOK, "")
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me/pro-devices/"+id, nil, h.auth(desktop.access)), http.StatusOK, "")
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me/pro-devices/"+id, nil, h.auth(desktop.access)), http.StatusOK, "")
	mustStatus(t, h.generate(mobile.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_REVOKED")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": mobile.refresh}, nil), http.StatusUnauthorized, "UNAUTHORIZED")
	mustStatus(t, h.generate(blocked.access, "Сәлем"), http.StatusOK, "")
	// A normal login must not let a revoked installation reclaim its old slot.
	again, _ := h.signInProDevice("pro-recovery@example.com", mobileResponse.str("device_id"), "ios", installID(841))
	mustStatus(t, h.generate(again.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_REVOKED")
	list := h.do(http.MethodGet, "/api/v1/me/pro-devices", nil, h.auth(desktop.access))
	for _, secret := range []string{installID(841), mobileResponse.str("device_id"), mobile.access, mobile.refresh} {
		if strings.Contains(string(list.raw), secret) {
			t.Fatal("device management disclosed a raw identity or credential")
		}
	}
}

func TestProDeviceAdminRecoveryRequiresSessionCSRFAndRole(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices())
	first, _ := h.signInProDevice("pro-admin@example.com", "admin-mobile-0001", "ios", installID(851))
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	id := h.proDeviceAuthorization(first.access, "mobile")
	path := "/api/v1/admin/users/" + first.userID + "/pro-devices"
	mustStatus(t, h.do(http.MethodGet, path, nil, nil), http.StatusUnauthorized, "UNAUTHORIZED")
	admin := h.signInAdmin()
	mustStatus(t, h.do(http.MethodGet, path, nil, admin.headers(h.cfg.Admin.CookieName)), http.StatusOK, "")
	noCSRF := map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie}
	if denied := h.do(http.MethodPost, path+"/"+id+"/revoke", map[string]any{"reason": "owner recovery"}, noCSRF); denied.status != http.StatusForbidden {
		t.Fatalf("admin mutation lacked CSRF protection: %d %s", denied.status, denied.raw)
	}
	h.exec(`UPDATE admin_users SET role='viewer' WHERE email=?`, adminEmail)
	if denied := h.do(http.MethodPost, path+"/"+id+"/revoke", map[string]any{"reason": "owner recovery"}, admin.headers(h.cfg.Admin.CookieName)); denied.status != http.StatusForbidden {
		t.Fatalf("viewer revoked Pro device: %d %s", denied.status, denied.raw)
	}
	h.exec(`UPDATE admin_users SET role='admin' WHERE email=?`, adminEmail)
	mustStatus(t, h.do(http.MethodPost, path+"/"+id+"/revoke", map[string]any{"reason": "owner recovery"}, admin.headers(h.cfg.Admin.CookieName)), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action='pro_device.revoked' AND entity_id=?`, id) != 1 {
		t.Fatal("administrator recovery was not audited")
	}
}
