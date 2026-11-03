package apptest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

func proNoticeDevices(t *testing.T, h *harness, s session, seed int) (domain.ProDeviceAuthorization, domain.Device) {
	t.Helper()
	h.notify.WithProDeviceNotificationDelivery(true)
	ctx := context.Background()
	deviceID := h.text(`SELECT id FROM devices WHERE user_id=? ORDER BY created_at LIMIT 1`, s.userID)
	a, _, err := h.store.ClaimProDevice(ctx, s.userID, deviceID, "mobile", "ios", "iPhone", h.pushClock.Now())
	if err != nil {
		t.Fatal(err)
	}
	h.mustRegister(installation(installID(seed), "ios", fcmToken(seed)), s.access)
	if err := h.store.BindProDeviceInstallation(ctx, s.userID, deviceID, installID(seed)); err != nil {
		t.Fatal(err)
	}
	blocked, err := h.store.UpsertDevice(ctx, domain.Device{UserID: s.userID, Platform: "android", Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	// This account also has a reachable, unauthorized installation. It must
	// never become a recipient through a default account-wide broadcast.
	h.mustRegister(installation(installID(seed+1), "android", fcmToken(seed+1)), s.access)
	if err := h.store.BindProDeviceInstallation(ctx, s.userID, blocked.ID, installID(seed+1)); err != nil {
		t.Fatal(err)
	}
	return a, blocked
}

func TestProDeviceNotificationDurableDeduplicatedAndTargeted(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("pro-notice@example.com")
	h.setPreferredLanguage(s.access, "ru")
	_, blocked := proNoticeDevices(t, h, s, 1801)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := h.notify.QueueProDeviceLimit(ctx, s.userID, blocked.ID, "mobile", "android", h.pushClock.Now(), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM pro_device_notification_jobs`) != 1 {
		t.Fatal("duplicate jobs")
	}
	if h.notices(s.userID, notifications.TypeProDeviceLimit) != 0 || len(h.push.messages()) != 0 || len(h.mail.messages()) != 0 {
		t.Fatal("queue performed delivery")
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	if h.notices(s.userID, notifications.TypeProDeviceLimit) != 1 {
		t.Fatal("pump duplicated notice")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.type=? AND d.channel='push' AND d.status='queued'`, notifications.TypeProDeviceLimit); n != 1 {
		t.Fatalf("push targets=%d", n)
	}
	h.tick()
	if len(h.push.messages()) != 1 || h.push.messages()[0].token != fcmToken(1801) {
		t.Fatal("notice reached unauthorized device")
	}
	msg := h.push.messages()[0].msg
	for _, value := range msg.Data {
		if value == blocked.ID {
			t.Fatal("device identifier exposed")
		}
	}
	if len(h.mail.messages()) != 1 {
		t.Fatal("email not delivered")
	}
	if got := h.text(`SELECT status FROM pro_device_notification_jobs`); got != "queued" {
		t.Fatalf("job=%s", got)
	}
}

func TestProDeviceNotificationNoTargetsNeverBroadcastsAndNoVerifiedEmail(t *testing.T) {
	h := newHarness(t)
	h.notify.WithProDeviceNotificationDelivery(true)
	s := h.signIn("no-target-pro@example.com")
	h.mustRegister(installation(installID(1821), "ios", fcmToken(1821)), s.access)
	deviceID := h.text(`SELECT id FROM devices WHERE user_id=? LIMIT 1`, s.userID)
	if _, err := h.db.Writer().Exec(`UPDATE users SET email_verified_at=NULL WHERE id=?`, s.userID); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.QueueProDeviceLimit(context.Background(), s.userID, deviceID, "mobile", "ios", h.pushClock.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if len(h.push.messages()) != 0 || len(h.mail.messages()) != 0 {
		t.Fatal("delivered without authorized target or verified address")
	}
	if got := h.text(`SELECT d.error_code FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.type=? AND d.channel='email'`, notifications.TypeProDeviceLimit); got != "no_verified_email" {
		t.Fatalf("email=%s", got)
	}
}

func TestProDeviceNotificationRetriesProviderFailures(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("retry-pro@example.com")
	_, blocked := proNoticeDevices(t, h, s, 1841)
	h.push.respond(push.Result{Outcome: push.Retry, Code: "UNAVAILABLE"})
	h.mail.failNext(errors.New("temporary provider failure"))
	if err := h.notify.QueueProDeviceLimit(context.Background(), s.userID, blocked.ID, "mobile", "android", h.pushClock.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.type=? AND d.status='retrying'`, notifications.TypeProDeviceLimit); got != 2 {
		t.Fatalf("retrying=%d", got)
	}
	h.pushClock.Advance(10 * time.Minute)
	h.tick()
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.type=? AND d.status='provider_accepted'`, notifications.TypeProDeviceLimit); got != 2 {
		t.Fatalf("accepted=%d", got)
	}
	if len(h.mail.messages()) != 1 {
		t.Fatal("email retry missing")
	}
}

func TestProDeviceNotificationCooldownIncludesAccountCeiling(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("cooldown-pro@example.com")
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		d, err := h.store.UpsertDevice(ctx, domain.Device{UserID: s.userID, Platform: "ios"})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.notify.QueueProDeviceLimit(ctx, s.userID, d.ID, "mobile", "ios", h.pushClock.Now(), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM pro_device_notification_jobs`); n != 3 {
		t.Fatalf("account cap=%d", n)
	}
	h.pushClock.Advance(time.Hour + time.Millisecond)
	d := h.text(`SELECT device_id FROM pro_device_notification_jobs LIMIT 1`)
	if err := h.notify.QueueProDeviceLimit(ctx, s.userID, d, "mobile", "ios", h.pushClock.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM pro_device_notification_jobs`); n != 4 {
		t.Fatalf("cooldown did not expire=%d", n)
	}
}

func TestProDeviceNotificationConcurrentQueueAndQueueFailureRecovery(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("concurrent-pro-notice@example.com")
	deviceID := h.text(`SELECT id FROM devices WHERE user_id=? LIMIT 1`, s.userID)
	ctx := context.Background()
	var wg sync.WaitGroup
	errorsCh := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsCh <- h.notify.QueueProDeviceLimit(ctx, s.userID, deviceID, "mobile", "ios", h.pushClock.Now(), time.Hour)
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := h.scalar(`SELECT COUNT(*) FROM pro_device_notification_jobs`); got != 1 {
		t.Fatalf("concurrent jobs=%d", got)
	}
	// A queueing outage must retain the durable job for another worker tick.
	if _, err := h.db.Writer().Exec(`ALTER TABLE notifications RENAME TO unavailable_pro_notices`); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err == nil {
		t.Fatal("expected queue failure")
	}
	if got := h.text(`SELECT status||':'||attempts FROM pro_device_notification_jobs`); got != "pending:1" {
		t.Fatalf("job after failure=%s", got)
	}
	if _, err := h.db.Writer().Exec(`ALTER TABLE unavailable_pro_notices RENAME TO notifications`); err != nil {
		t.Fatal(err)
	}
	h.pushClock.Advance(time.Minute)
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.notices(s.userID, notifications.TypeProDeviceLimit); got != 1 {
		t.Fatalf("recovered notices=%d", got)
	}
}

func TestProDeviceNotificationIntentRecoversRequestQueueFailureWithoutShadowReplay(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("intent-pro-notice@example.com")
	deviceID := h.text(`SELECT id FROM devices WHERE user_id=? LIMIT 1`, s.userID)
	ctx := context.Background()
	for _, e := range []domain.ProDeviceEvent{
		{Mode: "shadow", NotificationRequested: true},
		{Mode: "enforce", NotificationRequested: false},
		{Mode: "enforce", NotificationRequested: true},
	} {
		e.UserID, e.DeviceID, e.Category, e.Platform, e.Kind, e.CreatedAt = s.userID, deviceID, "mobile", "ios", "limit_reached", h.pushClock.Now()
		if err := h.store.RecordProDeviceEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM pro_device_events WHERE notify_requested=1`); n != 1 {
		t.Fatalf("intents=%d", n)
	}
	if _, err := h.db.Writer().Exec(`ALTER TABLE pro_device_notification_jobs RENAME TO unavailable_pro_jobs`); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.QueueProDeviceLimit(ctx, s.userID, deviceID, "mobile", "ios", h.pushClock.Now(), time.Hour); err == nil {
		t.Fatal("expected request queue failure")
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err == nil {
		t.Fatal("expected reconcile failure")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM pro_device_events WHERE notification_processed_at IS NOT NULL`); n != 0 {
		t.Fatalf("failed intent processed=%d", n)
	}
	if _, err := h.db.Writer().Exec(`ALTER TABLE unavailable_pro_jobs RENAME TO pro_device_notification_jobs`); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM pro_device_notification_jobs`); n != 1 {
		t.Fatalf("recovered jobs=%d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM pro_device_events WHERE notification_processed_at IS NOT NULL`); n != 1 {
		t.Fatalf("processed intents=%d", n)
	}
	if n := h.notices(s.userID, notifications.TypeProDeviceLimit); n != 1 {
		t.Fatalf("recovered notices=%d", n)
	}
}

func TestProDeviceNotificationRechecksRevocationAtDispatch(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("revoke-pro-notice@example.com")
	a, blocked := proNoticeDevices(t, h, s, 1861)
	ctx := context.Background()
	if err := h.notify.QueueProDeviceLimit(ctx, s.userID, blocked.ID, "mobile", "android", h.pushClock.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.store.RevokeProDevice(ctx, s.userID, a.ID, a.DeviceID, "replacement", false, h.pushClock.Now()); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if len(h.push.messages()) != 0 {
		t.Fatal("revoked installation received push")
	}
}

func TestProDeviceNotificationTemplates(t *testing.T) {
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"ru", "kk", "en"} {
		t.Run(locale, func(t *testing.T) {
			content, err := (email.NotificationTemplates{Translate: bundle.T}).Render(notifications.TypeProDeviceLimit, locale, map[string]string{"platform": "android", "category": bundle.T(locale, "pro_devices.category.mobile"), "date": "2026-11-03 18:00:00 +05"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(content.Text, "android") || !strings.Contains(content.Text, "2026-11-03") || strings.Contains(content.Text, "{category}") {
				t.Fatalf("incomplete template: %s", content.Text)
			}
		})
	}
}

func TestProDeviceNotificationRollbackStopsQueuedDelivery(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("rollback-pro-notice@example.com")
	_, blocked := proNoticeDevices(t, h, s, 1881)
	ctx := context.Background()
	if err := h.notify.QueueProDeviceLimit(ctx, s.userID, blocked.ID, "mobile", "android", h.pushClock.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := h.notify.PumpProDeviceNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	h.notify.WithProDeviceNotificationDelivery(false)
	h.tick()
	if len(h.push.messages()) != 0 || len(h.mail.messages()) != 0 {
		t.Fatal("rollback delivered queued notice")
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE error_code='pro_device_notifications_disabled'`); got != 2 {
		t.Fatalf("rollback skips=%d", got)
	}
}

func TestAdminProDevicesRequireRoleCSRFAndReasonAndAuditRevocation(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("admin-pro-device@example.com")
	deviceID := h.text(`SELECT id FROM devices WHERE user_id=? LIMIT 1`, s.userID)
	a, _, err := h.store.ClaimProDevice(context.Background(), s.userID, deviceID, "mobile", "ios", "iPhone", h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/users/" + s.userID + "/pro-devices"
	if r := h.do(http.MethodGet, path, nil, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("anonymous=%d", r.status)
	}
	admin := h.signInAdmin()
	headers := admin.headers(h.cfg.Admin.CookieName)
	if r := h.do(http.MethodGet, path, nil, headers); r.status != http.StatusOK || strings.Contains(string(r.raw), deviceID) {
		t.Fatalf("list=%d %s", r.status, r.raw)
	}
	revoke := fmt.Sprintf("%s/%s/revoke", path, a.ID)
	if r := h.do(http.MethodPost, revoke, map[string]any{"reason": "lost device"}, map[string]string{"Cookie": headers["Cookie"]}); r.status != http.StatusForbidden {
		t.Fatalf("no CSRF=%d", r.status)
	}
	if r := h.do(http.MethodPost, revoke, map[string]any{}, headers); r.status != http.StatusBadRequest {
		t.Fatalf("no reason=%d %s", r.status, r.raw)
	}
	if _, err := h.db.Writer().Exec(`UPDATE admin_users SET role='viewer' WHERE email=?`, adminEmail); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		p := path
		if method == http.MethodPost {
			p = revoke
		}
		if r := h.do(method, p, map[string]any{"reason": "lost device"}, headers); r.status != http.StatusForbidden {
			t.Fatalf("viewer %s=%d %s", method, r.status, r.raw)
		}
	}
	if _, err := h.db.Writer().Exec(`UPDATE admin_users SET role='admin' WHERE email=?`, adminEmail); err != nil {
		t.Fatal(err)
	}
	if r := h.do(http.MethodPost, revoke, map[string]any{"reason": "lost device"}, headers); r.status != http.StatusOK {
		t.Fatalf("revoke=%d %s", r.status, r.raw)
	}
	if h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action='pro_device.revoked' AND entity_id=?`, a.ID) != 1 {
		t.Fatal("missing transactional audit")
	}
	if h.scalar(`SELECT COUNT(*) FROM refresh_tokens WHERE user_id=? AND device_id=? AND revoked_at IS NULL`, s.userID, deviceID) != 0 {
		t.Fatal("sessions survived revocation")
	}
}

func TestProDevicePolishPreservesDeviceLimitErrorDetails(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices())
	first, _ := h.signInProDevice("polish-device-details@example.com", "polish-details-first-0001", "ios", installID(1891))
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	second, _ := h.signInProDevice("polish-device-details@example.com", "polish-details-second-0002", "android", installID(1892))
	r := h.polish(second.access, map[string]any{"text": polishNote, "input_language": "ru"})
	mustStatus(t, r, http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	if r.str("error", "details", "device_category") != "mobile" || r.num("error", "details", "allowed_devices") != 1 {
		t.Fatalf("missing device details: %s", r.raw)
	}
}
