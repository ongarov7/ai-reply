package domain

import (
	"slices"
	"testing"
	"time"
)

// Айлық шек күндік қалдықты азайтады; шексіз ай (0) күндікке әсер етпейді.
func TestEntitlementRemainingRespectsTheMonthlyCap(t *testing.T) {
	for name, tc := range map[string]struct {
		e    Entitlement
		want int
	}{
		"no plan limit":              {Entitlement{DailyLimit: 0}, 0},
		"daily only":                 {Entitlement{DailyLimit: 7, UsedToday: 2}, 5},
		"daily used up":              {Entitlement{DailyLimit: 7, UsedToday: 9}, 0},
		"unlimited month":            {Entitlement{DailyLimit: 30, UsedToday: 4, MonthlyLimit: 0, UsedMonth: 900}, 26},
		"month below day":            {Entitlement{DailyLimit: 30, UsedToday: 1, MonthlyLimit: 100, UsedMonth: 97}, 3},
		"month above day":            {Entitlement{DailyLimit: 30, UsedToday: 10, MonthlyLimit: 600, UsedMonth: 40}, 20},
		"month used up":              {Entitlement{DailyLimit: 30, UsedToday: 0, MonthlyLimit: 100, UsedMonth: 100}, 0},
		"month counter over the cap": {Entitlement{DailyLimit: 30, MonthlyLimit: 100, UsedMonth: 120}, 0},
	} {
		if got := tc.e.Remaining(); got != tc.want {
			t.Errorf("%s: Remaining() = %d, want %d", name, got, tc.want)
		}
	}
}

// Барлық қалдық = тариф (remaining_today) + pass + бонус + кредиттер; Spent уақыт белгісін ғана қалдырады.
func TestRemainingTotalAddsTheExtraBuckets(t *testing.T) {
	end := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	b := CreditBalance{HasWelcome: true, WelcomeTotal: 50, WelcomeRemaining: 35, Admin: 4, Packs: 6,
		PassActive: true, PassRemaining: 10, PassExpiresAt: &end}
	e := Entitlement{DailyLimit: 30, UsedToday: 1, MonthlyLimit: 100, UsedMonth: 97, Balance: b}
	if e.Remaining() != 3 || b.Credits() != 10 || b.Extra() != 55 || e.RemainingTotal() != 58 {
		t.Fatalf("remaining %d, credits %d, extra %d, total %d", e.Remaining(), b.Credits(), b.Extra(), e.RemainingTotal())
	}
	spent := b.Spent()
	if spent.Extra() != 0 || !spent.HasWelcome || spent.WelcomeTotal != 50 || !spent.PassActive || spent.PassExpiresAt != &end {
		t.Fatalf("spent = %+v", spent)
	}
}

// Deep link экрандары мен автоматты түрлер бір-бір реттен (бірнеше тармақ бір тізімге қосқан).
func TestLinkScreensAndAutomaticTypesAreListedOnce(t *testing.T) {
	for name, list := range map[string][]string{"LinkScreens": LinkScreens, "AutomaticNotificationTypes": AutomaticNotificationTypes} {
		seen := map[string]bool{}
		for _, v := range list {
			if seen[v] {
				t.Errorf("%s lists %q twice", name, v)
			}
			seen[v] = true
		}
	}
	for _, screen := range []string{"onboarding", "permissions", "paste_help", "support", "purchase", "keyboard"} {
		if !slices.Contains(LinkScreens, screen) {
			t.Errorf("LinkScreens has no %q", screen)
		}
	}
	for _, kind := range append(append([]string{}, AdminReminderTypes...), TypeSupportReply, TypeQuotaLow) {
		if !slices.Contains(AutomaticNotificationTypes, kind) {
			t.Errorf("AutomaticNotificationTypes has no %q", kind)
		}
	}
}
