package notifications

import (
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Шек: max(1, ceil(лимит × 10%)) — 7 → 1, 30 → 3, 50 → 5.
func TestQuotaLowThreshold(t *testing.T) {
	for limit, want := range map[int]int{1: 1, 7: 1, 10: 1, 11: 2, 30: 3, 50: 5, 100000: 10000} {
		if got := domain.QuotaLowThreshold(limit, 10); got != want {
			t.Errorf("limit %d: threshold %d, want %d", limit, got, want)
		}
	}
}

// Бір сұранысқа ең көбі бір квота хабарламасы; маңыздысы бірінші.
func TestQuotaNoticeArithmetic(t *testing.T) {
	e := NewEvents(testService())
	for _, tc := range []struct {
		name                               string
		daily, usedDay, monthly, usedMonth int
		wantKey, wantRemaining, wantLimit  string
	}{
		{name: "free, two left", daily: 7, usedDay: 5},
		{name: "free, last one", daily: 7, usedDay: 6, wantKey: "quota_low:day:2026-03-10", wantRemaining: "1", wantLimit: "7"},
		{name: "free, none left", daily: 7, usedDay: 7, wantKey: "quota_exhausted:day:2026-03-10", wantRemaining: "0", wantLimit: "7"},
		{name: "standard above", daily: 30, usedDay: 26},
		{name: "standard at threshold", daily: 30, usedDay: 27, wantKey: "quota_low:day:2026-03-10", wantRemaining: "3", wantLimit: "30"},
		{name: "pro at threshold", daily: 50, usedDay: 45, wantKey: "quota_low:day:2026-03-10", wantRemaining: "5", wantLimit: "50"},
		{name: "a lowered limit", daily: 50, usedDay: 60, wantKey: "quota_exhausted:day:2026-03-10", wantRemaining: "0", wantLimit: "50"},
		{name: "no limit set", daily: 0, usedDay: 3},
		{name: "monthly unlimited", daily: 50, usedDay: 1, monthly: 0, usedMonth: 900},
		{name: "day out beats month low", daily: 50, usedDay: 50, monthly: 100, usedMonth: 99,
			wantKey: "quota_exhausted:day:2026-03-10", wantRemaining: "0", wantLimit: "50"},
		{name: "month out beats everything", daily: 50, usedDay: 50, monthly: 100, usedMonth: 100,
			wantKey: "quota_exhausted:month:2026-03", wantRemaining: "0", wantLimit: "100"},
		{name: "month low before day low", daily: 50, usedDay: 46, monthly: 100, usedMonth: 95,
			wantKey: "quota_low:month:2026-03", wantRemaining: "5", wantLimit: "100"},
	} {
		n, ok := e.quotaNotice("user-1", domain.Entitlement{
			DailyLimit: tc.daily, UsedToday: tc.usedDay, MonthlyLimit: tc.monthly, UsedMonth: tc.usedMonth,
		}, "2026-03-10", "2026-03")
		if tc.wantKey == "" {
			if ok {
				t.Errorf("%s: unexpected %s", tc.name, n.IdempotencyKey)
			}
			continue
		}
		if !ok || n.IdempotencyKey != tc.wantKey || n.Params["remaining"] != tc.wantRemaining || n.Params["limit"] != tc.wantLimit ||
			n.Category != domain.CategorySubscription || n.Link != LinkSubscription {
			t.Errorf("%s: %+v", tc.name, n)
		}
	}
}

// Жадтағы жиын күн ауысқанда басынан басталады.
func TestRecentKeysStartOverEachDay(t *testing.T) {
	var r recentKeys
	r.add("2026-03-10", "a")
	if !r.has("2026-03-10", "a") || r.has("2026-03-10", "b") {
		t.Fatal("same day")
	}
	if r.has("2026-03-11", "a") {
		t.Fatal("a new day forgets the old keys")
	}
	r.add("2026-03-11", "b")
	if r.has("2026-03-11", "a") || !r.has("2026-03-11", "b") {
		t.Fatal("the set starts over")
	}
}
