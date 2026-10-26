package apptest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

func TestIOSPushAudienceExplainsThreeAndroidAndRecoversAfterPermissionChange(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ios-audience-recovery@example.com")
	for i := 901; i <= 903; i++ {
		h.mustRegister(installation(installID(i), "android", fcmToken(i)), s.access)
	}
	ios := installation(installID(904), "ios", fcmToken(904))
	ios["notification_permission"] = domain.PermissionNotDetermined
	h.mustRegister(ios, s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	preview := func() response {
		return h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
			map[string]any{"category": "system"}, headers)
	}
	before := preview()
	if before.status != http.StatusOK || before.num("preview", "android") != 3 || before.num("preview", "ios") != 0 ||
		before.num("preview", "matched_devices") != 4 || before.num("preview", "users") != 1 {
		t.Fatalf("three Android and a token-bearing iOS without permission: %d %s", before.status, before.raw)
	}
	if before.num("preview", "by_platform", "ios", "matched_devices") != 1 ||
		before.num("preview", "by_platform", "ios", "exclusions", "permission_denied", "devices") != 1 ||
		before.num("preview", "by_platform", "android", "devices") != 3 ||
		before.num("preview", "by_platform", "android", "users") != 1 {
		t.Fatalf("the iOS zero must be explained independently of Android: %s", before.raw)
	}
	// Metadata-only registration after the permission prompt must retain the
	// encrypted FCM token; a new token or a server-side consent bypass is unnecessary.
	h.mustRegister(installation(installID(904), "ios", ""), s.access)
	after := preview()
	if after.num("preview", "devices") != 4 || after.num("preview", "ios") != 1 ||
		after.num("preview", "by_platform", "ios", "users") != 1 ||
		after.num("preview", "by_platform", "ios", "devices") != 1 ||
		after.num("preview", "exclusions", "permission_denied", "devices") != 0 {
		t.Fatalf("authorized iOS must become reachable using its existing token: %s", after.raw)
	}
	campaign := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns",
		campaignBody(map[string]any{"send": true, "category": "system"}), withKey(headers, "ios-platform-recovery-0001"))
	if campaign.status != http.StatusCreated || campaign.num("device_count") != 4 || campaign.num("recipient_count") != 1 {
		t.Fatalf("campaign snapshot must agree with preview: %d %s", campaign.status, campaign.raw)
	}
	h.tick()
	sent := sentTokens(h.push)
	for i := 901; i <= 904; i++ {
		if sent[fcmToken(i)] != 1 {
			t.Fatalf("platform-neutral snapshot/dispatch must send once to each token: device %d, total %d", i, len(sent))
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE campaign_id = ? AND status = ?`,
		campaign.str("id"), domain.DeliveryAccepted) != 4 {
		t.Fatal("not all selected iOS and Android deliveries were accepted")
	}
}

func TestAudiencePlatformExclusionsKeepOverallUsersDistinctAndHonorFilters(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("platform-exclusion-counts@example.com")
	for i, platform := range []string{"ios", "android", "ios"} {
		body := installation(installID(910+i), platform, fcmToken(910+i))
		body["notification_permission"] = domain.PermissionDenied
		h.mustRegister(body, s.access)
	}
	// Anonymous token-bearing installations must not inflate campaign diagnostics.
	h.mustRegister(installation(installID(913), "ios", fcmToken(913)), "")
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	for _, platform := range []string{"", "ios", "android"} {
		t.Run(fmt.Sprintf("filter=%s", platform), func(t *testing.T) {
			audience := map[string]any{}
			if platform != "" {
				audience["platforms"] = []string{platform}
			}
			res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
				map[string]any{"category": "system", "audience": audience}, headers)
			if res.status != http.StatusOK || res.num("preview", "exclusions", "permission_denied", "users") != 1 {
				t.Fatalf("one account must stay one overall excluded user: %d %s", res.status, res.raw)
			}
			wantIOS, wantAndroid := 2.0, 1.0
			if platform == "ios" {
				wantAndroid = 0
			} else if platform == "android" {
				wantIOS = 0
			}
			if res.num("preview", "by_platform", "ios", "matched_devices") != wantIOS ||
				res.num("preview", "by_platform", "ios", "exclusions", "permission_denied", "devices") != wantIOS ||
				res.num("preview", "by_platform", "android", "matched_devices") != wantAndroid ||
				res.num("preview", "by_platform", "android", "exclusions", "permission_denied", "devices") != wantAndroid {
				t.Fatalf("platform diagnostics must honor filters and exclude anonymous devices: %s", res.raw)
			}
		})
	}
}

func TestAPNsCredentialFailurePreservesIOSFCMTokenAndAudience(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("ios-apns-credentials@example.com")
	h.mustRegister(installation(installID(920), "ios", fcmToken(920)), s.access)
	h.push.respond(push.Result{Outcome: push.Rejected, Code: "THIRD_PARTY_AUTH_ERROR", StatusCode: http.StatusUnauthorized})
	first := h.notifyUser(s.userID, "ios-apns-credentials:failure")
	h.tick()
	if got := h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE notification_id = ?`,
		first.NotificationID); got != domain.DeliveryFailed+":THIRD_PARTY_AUTH_ERROR" {
		t.Fatalf("APNs credentials must be reported as a provider failure: %s", got)
	}
	if got := h.text(`SELECT push_status FROM app_installations WHERE installation_id = ?`, installID(920)); got != domain.PushActive {
		t.Fatalf("an APNs credential failure must not deactivate the device token: %s", got)
	}
	preview := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"category": "system"},
		h.signInAdmin().headers(h.cfg.Admin.CookieName))
	if preview.status != http.StatusOK || preview.num("preview", "ios") != 1 {
		t.Fatalf("APNs credential configuration cannot explain an iOS zero in preview: %s", preview.raw)
	}
	// Once provider credentials are repaired, the same token works without
	// asking the app to rotate it or registering the installation again.
	second := h.notifyUser(s.userID, "ios-apns-credentials:repaired")
	h.tick()
	if got := h.text(`SELECT status FROM notification_deliveries WHERE notification_id = ?`, second.NotificationID); got != domain.DeliveryAccepted {
		t.Fatalf("push after credential repair: %s", got)
	}
	if sentTokens(h.push)[fcmToken(920)] != 2 {
		t.Fatal("the two attempts must use the same preserved FCM token")
	}
}
