package admin

import (
	"context"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// UserDetail — қолданушы картасы. Хабарлама тарихы ЖОҚ және болмайды.
type UserDetail struct {
	User          domain.User
	Profile       domain.Profile
	Entitlement   domain.Entitlement
	Subscriptions []domain.Subscription
	Devices       []domain.Device
	Events        []domain.UsageEvent
	Sessions      []repository.RefreshToken
	Payments      []domain.Payment
}

// Users — сүзгіленген тізім.
func (s *Service) Users(ctx context.Context, f repository.UserFilter) ([]repository.UserRow, int, error) {
	date, month := s.subs.TodayKeys()
	return s.repo.ListUsers(ctx, f, date, month)
}

// UserDetail — бір қолданушының метадерегі.
func (s *Service) UserDetail(ctx context.Context, id string) (UserDetail, error) {
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return UserDetail{}, err
	}
	detail := UserDetail{User: user}
	if detail.Profile, err = s.repo.Profile(ctx, id); err != nil {
		return UserDetail{}, err
	}
	if detail.Entitlement, err = s.subs.Entitlement(ctx, id); err != nil {
		return UserDetail{}, err
	}
	if detail.Subscriptions, err = s.repo.SubscriptionsByUser(ctx, id); err != nil {
		return UserDetail{}, err
	}
	if detail.Devices, err = s.repo.DevicesByUser(ctx, id); err != nil {
		return UserDetail{}, err
	}
	if detail.Events, err = s.repo.UserEvents(ctx, id, 20); err != nil {
		return UserDetail{}, err
	}
	if detail.Sessions, err = s.repo.ActiveSessions(ctx, id); err != nil {
		return UserDetail{}, err
	}
	if detail.Payments, err = s.repo.PaymentsByUser(ctx, id, 20); err != nil {
		return UserDetail{}, err
	}
	return detail, nil
}

// SetUserStatus — белсенді/өшірілген.
func (s *Service) SetUserStatus(ctx context.Context, admin domain.AdminUser, ip, userID, status string) error {
	if !traits.OneOf(status, domain.UserActive, domain.UserDisabled) {
		return domain.ErrInvalidRequest
	}
	if err := s.repo.UpdateUserStatus(ctx, userID, status); err != nil {
		return err
	}
	meta := map[string]any{"status": status}
	if status == domain.UserDisabled {
		if _, err := s.repo.RevokeUserSessions(ctx, userID, "account_disabled"); err != nil {
			return err
		}
		// A disabled account keeps no device: its notifications stop at once.
		detached, err := s.repo.DetachUserInstallations(ctx, userID, s.clock.Now())
		if err != nil {
			return err
		}
		meta["devices_detached"] = detached
	}
	s.Audit(ctx, admin, ip, "user.status", "user", userID, meta)
	return nil
}

// AssignPlan — тарифті ауыстыру.
func (s *Service) AssignPlan(ctx context.Context, admin domain.AdminUser, ip, userID, planID string, expires *time.Time) error {
	sub, err := s.subs.Assign(ctx, userID, planID, "admin", expires)
	if err != nil {
		return err
	}
	s.Audit(ctx, admin, ip, "subscription.assign", "user", userID, map[string]any{"plan_id": planID})
	if s.events != nil {
		s.events.PlanAssigned(ctx, sub)
	}
	return nil
}

// SetExpiry — жазылым мерзімін өзгерту.
func (s *Service) SetExpiry(ctx context.Context, admin domain.AdminUser, ip, userID string, expires *time.Time) error {
	if err := s.subs.SetExpiry(ctx, "", userID, expires); err != nil {
		return err
	}
	meta := map[string]any{"expires_at": nil}
	if expires != nil {
		meta["expires_at"] = expires.Format(time.RFC3339)
	}
	s.Audit(ctx, admin, ip, "subscription.expiry", "user", userID, meta)
	return nil
}

// ResetDailyQuota — бүгінгі квотаны нөлдеу.
func (s *Service) ResetDailyQuota(ctx context.Context, admin domain.AdminUser, ip, userID string) error {
	date, _ := s.subs.TodayKeys()
	if err := s.repo.ResetDailyQuota(ctx, userID, date); err != nil {
		return err
	}
	s.Audit(ctx, admin, ip, "usage.reset", "user", userID, map[string]any{"date": date})
	return nil
}

// RevokeSessions — барлық сессияны жабу.
func (s *Service) RevokeSessions(ctx context.Context, admin domain.AdminUser, ip, userID string) error {
	count, err := s.repo.RevokeUserSessions(ctx, userID, "admin_revoked")
	if err != nil {
		return err
	}
	// "Sign out everywhere" includes push: the phones stop receiving this
	// account's notifications until the person signs in again.
	detached, err := s.repo.DetachUserInstallations(ctx, userID, s.clock.Now())
	if err != nil {
		return err
	}
	s.Audit(ctx, admin, ip, "sessions.revoke", "user", userID,
		map[string]any{"revoked": count, "devices_detached": detached})
	return nil
}

// CreatePlan — тариф қосу.
func (s *Service) CreatePlan(ctx context.Context, admin domain.AdminUser, ip string, plan domain.Plan) (domain.Plan, error) {
	created, err := s.plans.Create(ctx, plan)
	if err != nil {
		return domain.Plan{}, err
	}
	s.Audit(ctx, admin, ip, "plan.create", "plan", created.ID, map[string]any{
		"code": created.Code, "daily_limit": created.DailyLimit, "price": created.Price,
		"is_active": created.IsActive, "is_visible": created.IsVisible,
	})
	return created, nil
}

// UpdatePlan — тарифті өзгерту.
func (s *Service) UpdatePlan(ctx context.Context, admin domain.AdminUser, ip string, plan domain.Plan) error {
	if err := s.plans.Update(ctx, plan); err != nil {
		return err
	}
	s.Audit(ctx, admin, ip, "plan.update", "plan", plan.ID, map[string]any{
		"code": plan.Code, "daily_limit": plan.DailyLimit, "price": plan.Price, "is_active": plan.IsActive,
		"is_visible": plan.IsVisible,
	})
	return nil
}

// IsDefaultPlan — жаңа қолданушыларға берілетін тариф пе.
func (s *Service) IsDefaultPlan(ctx context.Context, id string) (bool, error) {
	return s.plans.IsDefault(ctx, id)
}

// ArchivePlan — тарифті мұрағаттау.
func (s *Service) ArchivePlan(ctx context.Context, admin domain.AdminUser, ip, planID string) error {
	if err := s.plans.Archive(ctx, planID); err != nil {
		return err
	}
	s.Audit(ctx, admin, ip, "plan.archive", "plan", planID, nil)
	return nil
}

// Plans — толық тізім.
func (s *Service) Plans(ctx context.Context) ([]domain.Plan, error) { return s.plans.All(ctx) }

// Plan — бір тариф.
func (s *Service) Plan(ctx context.Context, id string) (domain.Plan, error) {
	return s.plans.Get(ctx, id)
}

// PlanUsage — тарифті пайдаланушылар саны.
func (s *Service) PlanUsage(ctx context.Context, id string) (int, error) {
	return s.plans.UsageCount(ctx, id)
}

// Pricing — модель бағалары.
func (s *Service) Pricing(ctx context.Context) ([]repository.Pricing, error) {
	return s.repo.ListPricing(ctx)
}

// SavePricing — жаңа баға кезеңі.
func (s *Service) SavePricing(ctx context.Context, admin domain.AdminUser, ip string, p repository.Pricing) error {
	if err := s.repo.SavePricing(ctx, p); err != nil {
		return err
	}
	s.Audit(ctx, admin, ip, "pricing.save", "model_pricing", p.Model, map[string]any{
		"input": p.InputPer1M, "output": p.OutputPer1M,
	})
	return nil
}
