package api

import (
	"context"
	"errors"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// usageForDevice reports what this authenticated device can spend without
// changing the account's subscription. Published clients cache this response,
// so an extra Pro device must not redisplay unlimited access after refreshing.
// Device IDs come only from the verified session (or freshly issued session),
// never a request body. Management responses remain successful on any denial.
func (s *Server) usageForDevice(ctx context.Context, user domain.User, deviceID string, ent domain.Entitlement) usageDTO {
	if s.proDevices != nil && ent.Unlimited {
		if err := s.proDevices.Authorize(ctx, user, deviceID, ent); errors.Is(err, domain.ErrProDeviceLimit) {
			// Identity, revocation and operational errors do not establish that
			// this device is eligible for the purchased-pack fallback.
			ent.Unlimited = false
			ent.DailyLimit, ent.MonthlyLimit, ent.DailyCreditGrant = 0, 0, 0
			ent.CreditsNeverExpire = false
			ent.Balance = domain.CreditBalance{Packs: ent.Balance.Packs}
		}
	}
	return s.usage(ent)
}
