package ai

import (
	"errors"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Only an occupied category slot can fall back to purchased replies. Revoked
// devices, inconsistent identities and storage failures must remain denied.
func purchasedDeviceFallback(ent domain.Entitlement, err error) bool {
	return ent.Unlimited && errors.Is(err, domain.ErrProDeviceLimit)
}

// This is request-scoped accounting, not a change to the paid subscription.
// The repository independently restricts the reservation and its replay to
// purchased packs; neither a plan allowance nor bonuses can pay for this call.
func purchasedOnlyEntitlement(ent domain.Entitlement) domain.Entitlement {
	ent.Unlimited = false
	ent.DailyLimit, ent.MonthlyLimit, ent.DailyCreditGrant = 0, 0, 0
	ent.CreditsNeverExpire = false
	ent.Balance = domain.CreditBalance{Packs: ent.Balance.Packs}
	return ent
}
