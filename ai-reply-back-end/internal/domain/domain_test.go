package domain

import "testing"

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
