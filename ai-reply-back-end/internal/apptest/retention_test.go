package apptest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/retention"
)

// Тазалау тек мерзімі өткен жолдарды өшіреді: жаңалары, аккаунтқа байланған
// орнатулар және жұмыс істеп тұрған сессия қалады.
func TestRetentionRemovesOnlyExpiredRows(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.signIn("retention-owner@example.com")
	now := h.clock.Now()
	ago := func(days int) int64 { return now.AddDate(0, 0, -days).UnixMilli() }
	ahead := now.AddDate(0, 0, 10).UnixMilli()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := h.db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	// Defaults: codes and dead sessions 30 days, anonymous phones 180, metadata 400.
	for id, created := range map[string]int64{"otp-old": ago(31), "otp-recent": ago(29)} {
		exec(`INSERT INTO otp_codes (id, identity_kind, identity_value, code_hash, expires_at, created_at)
			VALUES (?, 'email', 'retention-other@example.com', 'x', ?, ?)`, id, created+300_000, created)
	}
	for id, times := range map[string][2]any{
		"rt-expired-old":    {ago(31), nil},   // expired a month ago
		"rt-revoked-old":    {ahead, ago(31)}, // revoked a month ago, would still be valid
		"rt-expired-recent": {ago(29), nil},   // not yet
		"rt-revoked-recent": {ahead, ago(2)},  // not yet
		"rt-live":           {ahead, nil},     // an open session
	} {
		exec(`INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, issued_at, expires_at, revoked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, id, s.userID, "family-"+id, "hash-"+id, ago(60), times[0], times[1])
	}
	for id, row := range map[string][2]any{
		"inst-anon-old":    {nil, ago(181)},
		"inst-anon-recent": {nil, ago(179)},
		"inst-account-old": {s.userID, ago(400)}, // signed in: kept until the account goes
	} {
		exec(`INSERT INTO app_installations (id, installation_id, user_id, platform, first_seen_at, last_seen_at,
			created_at, updated_at) VALUES (?, ?, ?, 'android', ?, ?, ?, ?)`,
			id, "installation-"+id, row[0], row[1], row[1], row[1], row[1])
	}
	for id, created := range map[string]int64{"pe-old": ago(401), "pe-recent": ago(399)} {
		exec(`INSERT INTO product_events (id, user_id, name, created_at) VALUES (?, ?, 'onboarding_started', ?)`,
			id, s.userID, created)
	}
	for id, created := range map[string]int64{"ue-old": ago(401), "ue-recent": ago(399)} {
		exec(`INSERT INTO ai_usage_events (id, user_id, status, created_at) VALUES (?, ?, 'success', ?)`,
			id, s.userID, created)
	}

	svc := retention.New(h.store, h.cfg.Retention, discardLog()).WithClock(h.clock)
	got := svc.Sweep(ctx)
	want := retention.Result{"otp_codes": 1, "refresh_tokens": 2, "app_installations": 1,
		"product_events": 1, "ai_usage_events": 1}
	if len(got) != len(want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
	for table, n := range want {
		if got[table] != n {
			t.Errorf("%s: removed %d, want %d", table, got[table], n)
		}
	}
	for _, gone := range []string{"otp-old", "rt-expired-old", "rt-revoked-old", "inst-anon-old", "pe-old", "ue-old"} {
		n := h.scalar(`SELECT (SELECT COUNT(*) FROM otp_codes WHERE id = ?1) + (SELECT COUNT(*) FROM refresh_tokens WHERE id = ?1)
			+ (SELECT COUNT(*) FROM app_installations WHERE id = ?1) + (SELECT COUNT(*) FROM product_events WHERE id = ?1)
			+ (SELECT COUNT(*) FROM ai_usage_events WHERE id = ?1)`, gone)
		if n != 0 {
			t.Errorf("%s is still there", gone)
		}
	}
	if again := svc.Sweep(ctx); len(again) != 0 {
		t.Fatalf("a second pass removed %v", again)
	}
	// The session the person is using keeps working.
	h.refresh(s)

	// A window of 0 switches that table off.
	cfg := h.cfg.Retention
	cfg.ProductEventsDays = 0
	h.clock.Advance(1000 * 24 * time.Hour)
	off := retention.New(h.store, cfg, discardLog()).WithClock(h.clock).Sweep(ctx)
	if off["product_events"] != 0 || h.scalar(`SELECT COUNT(*) FROM product_events WHERE id = 'pe-recent'`) != 1 {
		t.Fatalf("product events removed with the window off: %v", off)
	}
	if off["ai_usage_events"] == 0 {
		t.Fatalf("the other windows still apply: %v", off)
	}
}

// Аккаунтсыз ескі орнатуды тазалау аяқталған науқанның есебін өзгертпейді.
func TestRetentionKeepsTheTotalsOfFinishedCampaigns(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		s := h.signIn(fmt.Sprintf("campaign-totals-%d@example.com", i))
		h.optInMarketing(s.access)
		h.mustRegister(installation(installID(130+i), "android", fcmToken(130+i)), s.access)
	}
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	id := h.do(http.MethodPost, "/api/v1/admin/notifications/campaigns", campaignBody(map[string]any{"send": true}),
		withKey(headers, "retention-totals-000001")).str("id")
	h.tick()
	before, err := h.notify.Campaign(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Campaign.Status != domain.CampaignCompleted || before.Campaign.FinalStats == nil ||
		before.Stats.Total != 2 || before.Stats.Accepted != 2 {
		t.Fatalf("campaign before retention: %s %+v", before.Campaign.Status, before.Stats)
	}

	// One recipient signed out long ago (or deleted the account): the phone is
	// anonymous and its last sighting is past the anonymous-installation window.
	if _, err := h.db.Writer().ExecContext(ctx, `UPDATE app_installations SET user_id = NULL, last_seen_at = ?
		WHERE installation_id = ?`, h.clock.Now().AddDate(0, 0, -200).UnixMilli(), installID(130)); err != nil {
		t.Fatal(err)
	}
	removed := retention.New(h.store, h.cfg.Retention, discardLog()).WithClock(h.clock).Sweep(ctx)
	if removed["app_installations"] != 1 || h.scalar(`SELECT COUNT(*) FROM notification_deliveries WHERE campaign_id = ?`, id) != 1 {
		t.Fatalf("the sweep did not take the installation and its delivery: %v", removed)
	}

	after, err := h.notify.Campaign(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	final := before.Campaign.FinalStats
	if after.Stats.Total != final.Total || after.Stats.Accepted != final.Accepted || after.Stats.Android != final.Android ||
		after.Stats.ByLanguage["kk"].Total != final.ByLanguage["kk"].Total {
		t.Fatalf("totals changed: %+v, final %+v", after.Stats, *final)
	}
	list := h.do(http.MethodGet, "/api/v1/admin/notifications/campaigns/"+id, nil, headers)
	if list.num("stats", "total") != 2 || list.num("stats", "provider_accepted") != 2 {
		t.Fatalf("admin view: %s", list.raw)
	}
}
