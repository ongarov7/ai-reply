package apptest

import (
	"net/http"
	"testing"
)

func TestProDeviceLegacyNonemptySignedIdentityCannotDowngrade(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices(), withEnv("PRO_DEVICE_LIMIT_LEGACY_SESSIONS", "true"))
	owner, original := h.signInProDevice("legacy-identity@example.com", "legacy-identity-owner", "ios", installID(970))
	h.assignPlan(h.signInAdmin(), owner.userID, "pro_monthly", "")
	mustStatus(t, h.generate(owner.access, "Сәлем"), http.StatusOK, "")
	authorization := h.proDeviceAuthorization(owner.access, "mobile")

	// This session has the owner's signed did but a different signed iid.
	// Compatibility must never turn its failed full binding into legacy access.
	clone, _ := h.signInProDevice("legacy-identity@example.com", original.str("device_id"), "ios", installID(971))
	before := h.provider.calls
	mustStatus(t, h.generate(clone.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	clone = h.refresh(clone)
	mustStatus(t, h.generate(clone.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	if h.provider.calls != before {
		t.Fatal("mismatched signed installation reached the provider")
	}
	if h.proDeviceAuthorization(owner.access, "mobile") != authorization {
		t.Fatal("mismatched signed installation changed slot ownership")
	}
	mustStatus(t, h.generate(owner.access, "Сәлем"), http.StatusOK, "")

	// The same rollout uses a client-safe refusal only for an empty iid.
	legacy, _ := h.signInProDevice("legacy-identity@example.com", "legacy-identity-second", "android", "")
	mustStatus(t, h.generate(legacy.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
}

func TestProDeviceLegacyFlagOffPreservesFullStrictIdentityPolicy(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices(), withEnv("PRO_DEVICE_LIMIT_LEGACY_SESSIONS", "false"))
	old, original := h.signInProDevice("legacy-flag-off@example.com", "legacy-flag-off-device", "ios", "")
	h.assignPlan(h.signInAdmin(), old.userID, "pro_monthly", "")
	h.mustRegister(installation(installID(972), "ios", ""), old.access)
	before := h.provider.calls
	mustStatus(t, h.generate(old.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	old = h.refresh(old)
	mustStatus(t, h.generate(old.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	if h.provider.calls != before || h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, old.userID) != 0 {
		t.Fatal("legacy access was admitted while its compatibility policy was off")
	}
	// The existing compatible-client contract still works under full strict.
	verified, _ := h.signInProDevice("legacy-flag-off@example.com", original.str("device_id"), "ios", installID(972))
	mustStatus(t, h.generate(verified.access, "Сәлем"), http.StatusOK, "")
}

func TestProDeviceLegacyNonProMultiplePhonesKeepExistingUsage(t *testing.T) {
	for _, plan := range []string{"free", "base_monthly"} {
		t.Run(plan, func(t *testing.T) {
			h := newHarness(t, approvedEntitlements(), strictProDevices(), withEnv("PRO_DEVICE_LIMIT_LEGACY_SESSIONS", "true"), withWelcomeBonus(10))
			first, _ := h.signInProDevice("legacy-nonpro@example.com", "legacy-nonpro-ios", "ios", "")
			second, _ := h.signInProDevice("legacy-nonpro@example.com", "legacy-nonpro-android", "android", "")
			if plan != "free" {
				h.assignPlan(h.signInAdmin(), first.userID, plan, "")
			}
			before := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(first.access))
			mustStatus(t, before, http.StatusOK, "")
			mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
			mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusOK, "")
			after := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(second.access))
			mustStatus(t, after, http.StatusOK, "")
			if after.body["unlimited"] == true {
				t.Fatal("compatibility changed a limited entitlement into unlimited access")
			}
			if plan == "free" {
				if h.welcomeLeft(first.userID) != 8 {
					t.Fatalf("two Free replies did not spend exactly two welcome units: %s", after.raw)
				}
			} else if after.num("used_today") != before.num("used_today")+2 || h.welcomeLeft(first.userID) != 10 {
				t.Fatalf("two Base replies changed existing daily accounting: before=%s after=%s", before.raw, after.raw)
			}
			if h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, first.userID) != 0 ||
				h.scalar(`SELECT COUNT(*) FROM pro_device_events WHERE user_id=?`, first.userID) != 0 {
				t.Fatal("non-Pro usage entered device enforcement")
			}
		})
	}
}

func TestProDeviceLegacyUnrelatedWebDeviceCanStillBeRemoved(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices(), withEnv("PRO_DEVICE_LIMIT_LEGACY_SESSIONS", "true"))
	owner, _ := h.signInProDevice("legacy-web-remove@example.com", "legacy-web-owner", "ios", "")
	web, original := h.signInProDevice("legacy-web-remove@example.com", "legacy-web-session", "web", "")
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/devices/"+original.str("device_id"), nil, h.auth(owner.access)), http.StatusOK, "")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": web.refresh}, nil), http.StatusUnauthorized, "UNAUTHORIZED")
}
