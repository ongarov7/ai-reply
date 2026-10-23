package apptest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

func TestAudienceExplainsMarketingOptInAndMasksRecipientSample(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("preview-consent@example.com")
	h.mustRegister(installation(installID(801), "ios", fcmToken(801)), s.access)
	h.mustRegister(installation(installID(802), "android", fcmToken(802)), "")
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	preview := func(audience map[string]any) response {
		return h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{
			"category": "marketing", "audience": audience,
		}, headers)
	}
	before := preview(nil)
	if before.status != http.StatusOK || before.num("preview", "matched_users") != 1 || before.num("preview", "matched_devices") != 1 ||
		before.num("preview", "users") != 0 || before.str("preview", "empty_reason") != "no_eligible_devices" ||
		before.num("preview", "exclusions", "category_not_opted_in", "devices") != 1 {
		t.Fatalf("missing opt-in must be explained; anonymous devices remain excluded: %d %s", before.status, before.raw)
	}
	h.optInMarketing(s.access)
	after := preview(nil)
	if after.num("preview", "users") != 1 || after.num("preview", "devices") != 1 || after.str("preview", "empty_reason") != "" {
		t.Fatalf("opted-in recipient: %s", after.raw)
	}
	records := after.body["preview"].(map[string]any)["records"].([]any)
	if len(records) != 1 || records[0].(map[string]any)["user_id"] != s.userID ||
		records[0].(map[string]any)["push"] != "fcm:"+push.TokenHash(fcmToken(801))[:8] {
		t.Fatalf("recipient sample: %s", after.raw)
	}
	for _, private := range []string{fcmToken(801), push.TokenHash(fcmToken(801)), installID(801), "preview-consent@example.com"} {
		if strings.Contains(string(after.raw), private) {
			t.Fatalf("preview exposed private registration data")
		}
	}
	if len(h.push.messages()) != 0 {
		t.Fatal("a preview sent a message")
	}
	if _, err := h.installations.Detach(context.Background(), installID(801), s.userID); err != nil {
		t.Fatal(err)
	}
	detached := preview(map[string]any{"user_ids": []string{s.userID}})
	if detached.num("preview", "matched_users") != 1 || detached.num("preview", "matched_devices") != 0 ||
		detached.str("preview", "empty_reason") != "no_devices" || detached.num("preview", "exclusions", "no_devices", "users") != 1 {
		t.Fatalf("account with no associated device: %s", detached.raw)
	}
	unknown := preview(map[string]any{"emails": []string{"missing@example.com"}})
	if unknown.str("preview", "empty_reason") != "no_matching_users" {
		t.Fatalf("unknown account: %s", unknown.raw)
	}
}

func TestAudienceExplainsDeviceEligibilityAndProviderConfiguration(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("preview-device-states@example.com")
	for i := 0; i < 5; i++ {
		body := installation(installID(810+i), "android", fcmToken(810+i))
		switch i {
		case 0:
			delete(body, "push")
		case 1:
			body["notification_permission"] = "denied"
		case 2:
			body["notifications_enabled"] = false
		}
		h.mustRegister(body, s.access)
	}
	if _, err := h.db.Writer().Exec(`UPDATE app_installations SET last_seen_at = ? WHERE installation_id = ?`,
		h.pushClock.Now().Add(-271*24*time.Hour).UnixMilli(), installID(813)); err != nil {
		t.Fatal(err)
	}
	res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"category": "system"},
		h.signInAdmin().headers(h.cfg.Admin.CookieName))
	if res.status != http.StatusOK || res.num("preview", "devices") != 1 || res.num("preview", "matched_devices") != 5 {
		t.Fatalf("preview: %s", res.raw)
	}
	for _, code := range []string{"token_inactive", "permission_denied", "disabled_in_app", "stale_device"} {
		if res.num("preview", "exclusions", code, "devices") != 1 {
			t.Errorf("missing exclusion %s: %s", code, res.raw)
		}
	}
	off := newHarness(t, withEnv("PUSH_NOTIFICATIONS_ENABLED", "false"))
	person := off.signIn("provider-disabled@example.com")
	off.mustRegister(installation(installID(820), "ios", fcmToken(820)), person.access)
	disabled := off.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview", map[string]any{"category": "system"},
		off.signInAdmin().headers(off.cfg.Admin.CookieName))
	if disabled.num("preview", "devices") != 0 || disabled.num("preview", "exclusions", "provider_unavailable", "devices") != 1 {
		t.Fatalf("disabled provider must explain zero: %s", disabled.raw)
	}
}

func TestCampaignRecipientsAreFrozenAtSendBeforeWorkerStarts(t *testing.T) {
	h := newHarness(t)
	first := h.signIn("snapshot-first@example.com")
	h.optInMarketing(first.access)
	h.mustRegister(installation(installID(830), "ios", fcmToken(830)), first.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	created := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "snapshot-at-send-0001"))
	if created.status != http.StatusCreated || created.str("status") != domain.CampaignQueued ||
		created.num("recipient_count") != 1 || created.num("device_count") != 1 || created.str("recipient_snapshot_at") == "" {
		t.Fatalf("queued campaign must expose its snapshot immediately: %d %s", created.status, created.raw)
	}
	id := created.str("id")
	if len(h.push.messages()) != 0 {
		t.Fatal("HTTP preparation contacted the provider")
	}
	second := h.signIn("snapshot-late@example.com")
	h.optInMarketing(second.access)
	h.mustRegister(installation(installID(831), "android", fcmToken(831)), second.access)
	// A rotated token on the same selected device remains valid; a newly added device is not added to this send.
	h.mustRegister(installation(installID(830), "ios", fcmToken(832)), first.access)
	again := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns/"+id+"/send", map[string]any{}, headers)
	if again.num("device_count") != 1 {
		t.Fatalf("retry changed snapshot: %s", again.raw)
	}
	h.tick()
	if got := sentTokens(h.push); len(got) != 1 || got[fcmToken(832)] != 1 {
		t.Fatalf("snapshot dispatch: %v", got)
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE campaign_id = ?`, id); got != 1 {
		t.Fatalf("snapshot rows: %d", got)
	}
}

func TestCampaignSnapshotRollsBackWithQueueOnInsertionFailure(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("snapshot-rollback@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(840), "android", fcmToken(840)), s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	draft := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), withKey(headers, "snapshot-rollback-0001"))
	id := draft.str("id")
	if _, err := h.db.Writer().Exec(`CREATE TRIGGER reject_snapshot BEFORE INSERT ON notification_deliveries BEGIN SELECT RAISE(ABORT, 'isolated test failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := h.notify.SendCampaign(context.Background(), id)
	if err == nil {
		t.Fatal("insertion failure was ignored")
	}
	if got := h.text(`SELECT status FROM notification_campaigns WHERE id = ?`, id); got != domain.CampaignDraft {
		t.Fatalf("partially queued: %s", got)
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notifications WHERE campaign_id = ?`, id); got != 0 {
		t.Fatal("partial notifications survived rollback")
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_campaigns WHERE id = ? AND recipient_snapshot_at IS NULL`, id); got != 1 {
		t.Fatal("partial snapshot marker survived rollback")
	}
	if _, err := h.db.Writer().Exec(`DROP TRIGGER reject_snapshot`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.notify.SendCampaign(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if len(h.push.messages()) != 1 {
		t.Fatal("retry after rollback did not send once")
	}
}

func TestCampaignSnapshotStillHonorsOptOutBeforeDispatch(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("snapshot-optout@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(850), "ios", fcmToken(850)), s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	c := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}), withKey(headers, "snapshot-optout-0001"))
	if res := h.do(http.MethodPut, "/api/v1/me/notification-preferences", map[string]any{"preferences": map[string]any{"marketing": false}}, h.auth(s.access)); res.status != http.StatusOK {
		t.Fatal("opt out failed")
	}
	h.tick()
	if len(h.push.messages()) != 0 {
		t.Fatal("snapshot ignored later consent withdrawal")
	}
	if got := h.text(`SELECT error_code FROM notification_deliveries WHERE campaign_id = ?`, c.str("id")); got != "disabled_by_user" {
		t.Fatalf("opt-out reason: %s", got)
	}
}

func TestLegacyQueuedCampaignGetsOneSnapshot(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("legacy-snapshot@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(860), "android", fcmToken(860)), s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	c := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), withKey(headers, "snapshot-legacy-0001"))
	if _, err := h.db.Writer().Exec(`UPDATE notification_campaigns SET status='queued', queued_at=? WHERE id=?`, h.pushClock.Now().UnixMilli(), c.str("id")); err != nil {
		t.Fatal(err)
	}
	h.tick()
	h.tick()
	if len(h.push.messages()) != 1 {
		t.Fatal("legacy queued campaign did not send exactly once")
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_campaigns WHERE id=? AND recipient_snapshot_at IS NOT NULL`, c.str("id")); got != 1 {
		t.Fatal("legacy snapshot not recorded")
	}
}

func TestEmptyCampaignSnapshotDoesNotAcquireLaterDevices(t *testing.T) {
	h := newHarness(t)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	c := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}), withKey(headers, "snapshot-empty-0001"))
	if c.status != http.StatusCreated || c.num("device_count") != 0 || c.str("recipient_snapshot_at") == "" {
		t.Fatalf("empty snapshot: %s", c.raw)
	}
	s := h.signIn("later-than-empty-snapshot@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(870), "android", fcmToken(870)), s.access)
	h.tick()
	if len(h.push.messages()) != 0 {
		t.Fatal("a frozen empty campaign acquired a later recipient")
	}
	final := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+c.str("id"), nil, headers)
	if final.str("status") != domain.CampaignCompleted || final.num("recipient_count") != 0 || final.num("stats", "total") != 0 {
		t.Fatalf("empty history: %s", final.raw)
	}
}

func TestConcurrentCampaignSendCreatesOneRecipientSnapshot(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("concurrent-snapshot@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(880), "android", fcmToken(880)), s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	draft := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(nil), withKey(headers, "snapshot-concurrent-0001"))
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := h.notify.SendCampaign(context.Background(), draft.str("id"))
			results <- result.Queued
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	queued := 0
	for result := range results {
		if result {
			queued++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if queued != 1 {
		t.Fatalf("queued transitions: %d", queued)
	}
	if count := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE campaign_id=?`, draft.str("id")); count != 1 {
		t.Fatalf("duplicate snapshot deliveries: %d", count)
	}
	h.tick()
	if len(h.push.messages()) != 1 {
		t.Fatal("concurrent sends produced duplicate push attempts")
	}
}

func TestCampaignSnapshotDeliveriesWaitForProcessingWhileAutomaticSendsContinue(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("snapshot-worker-gate@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(890), "ios", fcmToken(890)), s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	for i := 0; i < 6; i++ {
		c := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
			withKey(headers, fmt.Sprintf("snapshot-worker-gate-%04d", i)))
		if c.status != http.StatusCreated {
			t.Fatalf("campaign %d: %d %s", i, c.status, c.raw)
		}
	}
	h.notifyUser(s.userID, "snapshot-worker-gate:auto")
	// A batch running in another worker before fan-out may claim automatic rows,
	// but must leave all six queued campaign snapshots untouched.
	if count, err := h.notify.ProcessBatch(context.Background()); err != nil || count != 1 {
		t.Fatalf("batch before campaign processing: count=%d err=%v", count, err)
	}
	if len(h.push.messages()) != 1 {
		t.Fatal("campaign sent before its processing transition")
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE campaign_id IS NOT NULL AND status='queued' AND attempt_count=0`); got != 6 {
		t.Fatalf("queued snapshots consumed prematurely: %d", got)
	}
	// One Tick starts at most five campaigns; the sixth remains safely queued.
	h.tick()
	if len(h.push.messages()) != 6 {
		t.Fatalf("first tick should send automatic + five campaigns, got %d", len(h.push.messages()))
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries d JOIN notification_campaigns c ON c.id=d.campaign_id
  WHERE c.status='queued' AND d.status='queued' AND d.attempt_count=0`); got != 1 {
		t.Fatalf("sixth campaign was not preserved: %d", got)
	}
	h.tick()
	if len(h.push.messages()) != 7 {
		t.Fatal("remaining campaign did not send exactly once on next tick")
	}
}

func TestEmailOnlyWorkerPreservesQueuedAndProcessingPushSnapshots(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("snapshot-email-only@example.com")
	h.optInMarketing(s.access)
	h.mustRegister(installation(installID(891), "android", fcmToken(891)), s.access)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	var ids []string
	for i := 0; i < 2; i++ {
		c := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
			withKey(headers, fmt.Sprintf("snapshot-email-only-%04d", i)))
		if c.status != http.StatusCreated {
			t.Fatalf("campaign: %d %s", c.status, c.raw)
		}
		ids = append(ids, c.str("id"))
	}
	// Another push-capable process has already started one of the campaigns.
	if _, err := h.db.Writer().Exec(`UPDATE notification_campaigns SET status='processing',started_at=? WHERE id=?`, h.pushClock.Now().UnixMilli(), ids[1]); err != nil {
		t.Fatal(err)
	}
	notice := accountNotice(s.userID, "snapshot-email-only:auto")
	notice.Email = true
	if _, err := h.notify.NotifyUser(context.Background(), notice); err != nil {
		t.Fatal(err)
	}
	cfg := h.cfg.Push
	cfg.Enabled = false
	emailOnly := notifications.New(notifications.Deps{
		Repo: h.store, Installations: h.installations, Config: cfg, Clock: h.pushClock, Log: discardLog(),
	}).WithEmail(h.mail, fakeEmailRenderer{})
	emailOnly.Tick(context.Background())
	if len(h.mail.messages()) != 1 {
		t.Fatal("email-only worker did not send its automatic email")
	}
	if len(h.push.messages()) != 0 {
		t.Fatal("email-only worker attempted push")
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE channel='push' AND status='queued' AND attempt_count=0`); got != 3 {
		t.Fatalf("email-only worker consumed queued push rows: %d", got)
	}
	if got := h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE error_code='provider_unavailable'`); got != 0 {
		t.Fatal("push rows were permanently skipped")
	}
	h.tick()
	if len(h.push.messages()) != 3 {
		t.Fatalf("push-capable worker could not resume preserved work: %d", len(h.push.messages()))
	}
}
