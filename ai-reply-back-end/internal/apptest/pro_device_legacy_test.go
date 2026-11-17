package apptest

import (
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/audio/audiotest"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func legacyProDevices() harnessOption {
	return func(env map[string]string) {
		strictProDevices()(env)
		env["PRO_DEVICE_LIMIT_LEGACY_SESSIONS"] = "true"
	}
}

// These requests have the published mobile contract: no installation identity
// at login, refresh, or AI generation. App and keyboard share one session.
func TestProLegacySessionDeviceCategories(t *testing.T) {
	for _, plan := range []string{"pro_monthly", "pro_yearly"} {
		for _, pair := range [][2]string{{"ios", "android"}, {"android", "ios"}, {"android", "android"}, {"ios", "ios"}, {"ios", "macos"}, {"android", "windows"}, {"macos", "macos"}} {
			t.Run(plan+"/"+pair[0]+"_"+pair[1], func(t *testing.T) {
				h := newHarness(t, approvedEntitlements(), legacyProDevices())
				first, _ := h.signInProDevice("legacy-pair@example.com", "legacy-first", pair[0], "")
				h.assignPlan(h.signInAdmin(), first.userID, plan, "")
				first = h.refresh(first)
				mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
				second, _ := h.signInProDevice("legacy-pair@example.com", "legacy-second", pair[1], "")
				blocked := (pair[0] == "macos") == (pair[1] == "macos" || pair[1] == "windows")
				if blocked {
					mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
				} else {
					mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusOK, "")
				}
				mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
				mustStatus(t, h.do(http.MethodGet, "/api/v1/me", nil, h.auth(second.access)), http.StatusOK, "")
			})
		}
	}
}

func TestProLegacyDenialPrecedesEveryProviderAndPreservesBilling(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), legacyProDevices(), withWelcomeBonus(10))
	first, _ := h.signInProDevice("legacy-endpoints@example.com", "legacy-endpoint-first", "ios", "")
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	h.addGrant(first.userID, domain.GrantAdmin, 30, h.clock.Now(), nil, h.clock.Now())
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	second, secondResponse := h.signInProDevice("legacy-endpoints@example.com", "legacy-endpoint-second", "android", "")
	before := h.provider.calls
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.compose(second.access, map[string]any{"instruction": "Әріптесімді құттықта", "language": "kk", "platform": "macos"}), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.polish(second.access, map[string]any{"text": polishNote, "input_language": "ru", "platform": "windows"}), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.transcribe(second.access, audiotest.M4A(2*time.Second, nil), voiceFields()), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	if h.provider.calls != before || len(h.speech.requests()) != 0 {
		t.Fatal("blocked old-style session reached an AI provider")
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(second.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("admin_credits") != 0 || usage.num("purchased_credits") != 0 || usage.num("welcome", "remaining") != 0 || usage.body["unlimited"] != false {
		t.Fatalf("additional legacy device advertises unusable benefits: %s", usage.raw)
	}
	ownerUsage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(first.access))
	if ownerUsage.num("admin_credits") != 30 || ownerUsage.num("welcome", "remaining") != 10 || ownerUsage.body["unlimited"] != true {
		t.Fatalf("denial modified the paid account or credits: %s", ownerUsage.raw)
	}
	refreshed := h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": second.refresh,
		"device":        map[string]any{"device_id": "legacy-endpoint-first", "platform": "macos"},
	}, nil)
	mustStatus(t, refreshed, http.StatusOK, "")
	if refreshed.str("device_id") != secondResponse.str("device_id") {
		t.Fatal("refresh trusted a replacement device ID")
	}
	mustStatus(t, h.generate(refreshed.str("access_token"), "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.generate(h.refresh(first).access, "Сәлем"), http.StatusOK, "")
}

func TestProLegacyAccountSwitchRecoversOriginalPhone(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), legacyProDevices())
	first, original := h.signInProDevice("legacy-return@example.com", "legacy-return-first", "ios", "")
	h.mustRegister(installation(installID(1101), "ios", ""), first.access)
	admin := h.signInAdmin()
	h.assignPlan(admin, first.userID, "pro_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	authorization := h.proDeviceAuthorization(first.access, "mobile")
	other, otherResponse := h.signInProDevice("legacy-other@example.com", original.str("device_id"), "ios", "")
	h.mustRegister(installation(installID(1101), "ios", ""), other.access)
	returned, returnedResponse := h.signInProDevice("legacy-return@example.com", otherResponse.str("device_id"), "ios", "")
	if returnedResponse.str("device_id") == original.str("device_id") {
		t.Fatal("fixture did not reproduce released-client account switching")
	}
	h.mustRegister(installation(installID(1101), "ios", ""), returned.access)
	mustStatus(t, h.generate(returned.access, "Сәлем"), http.StatusOK, "")
	mustStatus(t, h.generate(h.refresh(returned).access, "Сәлем"), http.StatusOK, "")
	if h.proDeviceAuthorization(returned.access, "mobile") != authorization || h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, first.userID) != 1 {
		t.Fatal("account switching displaced original device authorization")
	}
	blocked, _ := h.signInProDevice("legacy-return@example.com", "legacy-different-phone", "ios", "")
	h.mustRegister(installation(installID(1102), "ios", ""), blocked.access)
	mustStatus(t, h.generate(blocked.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me/pro-devices/"+authorization, nil, h.auth(blocked.access)), http.StatusForbidden, "PRO_DEVICE_MANAGEMENT_FORBIDDEN")
	// Support revokes the canonical slot and every alias refresh family.
	revoke := h.adminCall(admin, http.MethodPost, "/users/"+first.userID+"/pro-devices/"+authorization+"/revoke", map[string]any{"reason": "owner requested replacement"})
	mustStatus(t, revoke, http.StatusOK, "")
	mustStatus(t, h.generate(returned.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_REVOKED")
}

func TestProLegacyAppleMonthlyAndYearlyUseExistingPurchaseEntitlement(t *testing.T) {
	for _, code := range []string{"pro_monthly", "pro_yearly"} {
		t.Run(code, func(t *testing.T) {
			h := newHarness(t, approvedEntitlements(), legacyProDevices(), withIAP(t))
			h.enableApprovedProducts()
			first, _ := h.signInProDevice("legacy-apple@example.com", "legacy-apple-first", "ios", "")
			tx := h.subscriptionTx(first.userID, "legacy-apple-tx", "legacy-apple-root", code, h.clock.Now(), h.clock.Now().Add(365*24*time.Hour), nil)
			mustStatus(t, h.postApple(first.access, tx), http.StatusOK, "")
			// The buyer owns the slot before making their first AI request.
			h.proDeviceAuthorization(first.access, "mobile")
			second, _ := h.signInProDevice("legacy-apple@example.com", "legacy-apple-second", "android", "")
			mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
			mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
			mustStatus(t, h.postApple(second.access, tx), http.StatusOK, "")
			mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
			mustStatus(t, h.do(http.MethodGet, "/api/v1/me", nil, h.auth(second.access)), http.StatusOK, "")
		})
	}
}
