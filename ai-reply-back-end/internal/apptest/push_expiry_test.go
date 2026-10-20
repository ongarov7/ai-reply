package apptest

import (
	"context"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

// Кезекте мерзімі өтіп кеткен push жіберілмейді (expired), қалған TTL провайдерге беріледі.
func TestStalePushIsSkippedAsExpired(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("stale-push@example.com")
	h.mustRegister(installation(installID(981), "android", fcmToken(981)), s.access)

	h.notifyUser(s.userID, "ttl:fresh")
	h.pushClock.Advance(time.Hour)
	h.tick()
	if msgs := h.push.messages(); len(msgs) != 1 || msgs[0].msg.TTL != notifications.DefaultTTL-time.Hour {
		t.Fatalf("remaining TTL: %+v", msgs)
	}

	h.notifyUser(s.userID, "ttl:stale")
	h.pushClock.Advance(25 * time.Hour)
	h.tick()
	if n := len(h.push.messages()); n != 1 {
		t.Fatalf("an expired push was sent (%d sends)", n)
	}
	if got := h.text(`SELECT d.status || ':' || d.error_code FROM notification_deliveries d
		JOIN notifications n ON n.id = d.notification_id WHERE n.idempotency_key = 'ttl:stale'`); got != "skipped:expired" {
		t.Fatalf("delivery = %s", got)
	}
}

// Квота хабары тек өз күнінің соңына дейін жарамды (қолданба белдеуінде).
func TestQuotaNoticeExpiresWithItsDay(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("quota-ttl@example.com")
	h.mustRegister(installation(installID(982), "android", fcmToken(982)), s.access)
	// 10:00 UTC is 15:00 in Almaty: the day ends in 9 hours.
	h.pushClock.Advance(time.Hour)
	if _, err := h.notify.NotifyUser(context.Background(), notifications.UserNotification{
		UserID: s.userID, IdempotencyKey: "quota_low:day:2026-03-10", Type: domain.TypeQuotaLow,
		Category: domain.CategorySubscription, Link: "aireply://home",
		Text: func(string) (string, string) { return "Квота", "Аз қалды" },
	}); err != nil {
		t.Fatal(err)
	}
	h.push.respond(push.Result{Outcome: push.Retry, Code: "UNAVAILABLE", StatusCode: 503})
	h.tick()
	if msgs := h.push.messages(); len(msgs) != 1 || msgs[0].msg.TTL != 9*time.Hour {
		t.Fatalf("first attempt: %+v", msgs)
	}
	h.pushClock.Advance(10 * time.Hour) // past midnight in Almaty
	h.tick()
	if n := len(h.push.messages()); n != 1 {
		t.Fatalf("a quota notice was retried after its day (%d sends)", n)
	}
	if got := h.text(`SELECT status || ':' || error_code FROM notification_deliveries WHERE user_id = ?`, s.userID); got != "skipped:expired" {
		t.Fatalf("delivery = %s", got)
	}
}

// Құрылғысы жоқ тіркелгі — no_devices, құрылғысы бар, бірақ жеткізуге болмайтыны — no_reachable_device.
func TestNoDevicesIsNotNoReachableDevice(t *testing.T) {
	h := newHarness(t)
	never := h.signIn("never-installed@example.com")
	if res := h.notifyUser(never.userID, "reach:1"); res.Skipped != notifications.SkipNoDevices {
		t.Fatalf("no installation: %+v", res)
	}
	stale := h.signIn("stale-device@example.com")
	h.mustRegister(installation(installID(983), "ios", fcmToken(983)), stale.access)
	h.pushClock.Advance(notifications.StaleAfter + time.Hour)
	if res := h.notifyUser(stale.userID, "reach:2"); res.Skipped != notifications.SkipNoReachableDevice {
		t.Fatalf("unseen installation: %+v", res)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE error_code = 'no_reachable_device'`); n != 1 {
		t.Fatalf("rows = %d", n)
	}
}
