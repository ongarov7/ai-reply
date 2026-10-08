package apptest

import (
	"context"
	"testing"
	"time"

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
