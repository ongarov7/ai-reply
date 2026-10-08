// Package subscriptions — жазылым және қолданушының нақты құқығы (entitlement).
package subscriptions

import (
	"context"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Service — жазылымдар.
type Service struct {
	repo  *repository.Store
	plans *plans.Service
	loc   *time.Location
	clock traits.Clock
}

// New — қызмет. loc — күндік квота қайта жаңаратын белдеу.
func New(repo *repository.Store, planSvc *plans.Service, loc *time.Location) *Service {
	return &Service{repo: repo, plans: planSvc, loc: loc, clock: traits.SystemClock{}}
}

// WithClock — тестке арналған.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// EnsureSubscription — жаңа қолданушыға тегін тариф береді.
func (s *Service) EnsureSubscription(ctx context.Context, userID string) error {
	if _, err := s.repo.CurrentSubscription(ctx, userID); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	plan, err := s.plans.Default(ctx)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateSubscription(ctx, domain.Subscription{
		UserID:    userID,
		PlanID:    plan.ID,
		Status:    domain.SubActive,
		Source:    "system",
		StartedAt: s.clock.Now(),
	})
	return err
}

// Current — ағымдағы жазылым (болмаса — ErrNotFound).
func (s *Service) Current(ctx context.Context, userID string) (domain.Subscription, error) {
	return s.repo.CurrentSubscription(ctx, userID)
}

// History — жазылым тарихы.
func (s *Service) History(ctx context.Context, userID string) ([]domain.Subscription, error) {
	return s.repo.SubscriptionsByUser(ctx, userID)
}

// Assign — тарифті ауыстыру (әкімші не төлем нәтижесі).
func (s *Service) Assign(ctx context.Context, userID, planID, source string, expires *time.Time) (domain.Subscription, error) {
	plan, err := s.repo.Plan(ctx, planID)
	if err != nil {
		return domain.Subscription{}, err
	}
	now := s.clock.Now()
	return s.repo.ReplaceSubscription(ctx, userID, domain.Subscription{
		UserID:    userID,
		PlanID:    plan.ID,
		Status:    domain.SubActive,
		Source:    source,
		StartedAt: now,
		ExpiresAt: periodEnd(plan, now, expires),
	})
}

// Renew — ағымдағы жарамды жазылым дәл осы тариф болса, тек мерзімін ауыстырады.
//
// true means it did: the person keeps the one subscription they have (and
// gets no second "plan active" notice). false means the current plan is a
// different one, or no longer usable, and the caller should Assign instead.
// The end date follows the same rule as Assign: without one, a paid plan
// with a period runs one period from now.
func (s *Service) Renew(ctx context.Context, userID, planID string, expires *time.Time) (bool, error) {
	current, err := s.repo.CurrentSubscription(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := s.clock.Now()
	if current.PlanID != planID || !current.IsUsable(now) {
		return false, nil
	}
	plan, err := s.repo.Plan(ctx, planID)
	if err != nil {
		return false, err
	}
	return true, s.SetExpiry(ctx, current.ID, userID, periodEnd(plan, now, expires))
}

// periodEnd — берілген мерзім, болмаса ақылы кезеңді тарифке бүгіннен бастап бір кезең.
func periodEnd(plan domain.Plan, now time.Time, expires *time.Time) *time.Time {
	if expires == nil && !plan.IsFree && plan.PeriodDays > 0 {
		end := now.AddDate(0, 0, plan.PeriodDays)
		return &end
	}
	return expires
}

// SetExpiry — мерзімді өзгерту.
func (s *Service) SetExpiry(ctx context.Context, subscriptionID, userID string, expires *time.Time) error {
	sub, err := s.repo.CurrentSubscription(ctx, userID)
	if err != nil {
		return err
	}
	if subscriptionID != "" && sub.ID != subscriptionID {
		return domain.ErrNotFound
	}
	sub.ExpiresAt = expires
	if expires != nil && expires.Before(s.clock.Now()) {
		sub.Status = domain.SubExpired
	} else if sub.Status == domain.SubExpired {
		sub.Status = domain.SubActive
	}
	return s.repo.UpdateSubscription(ctx, sub)
}

// Cancel — жазылымды тоқтату.
func (s *Service) Cancel(ctx context.Context, userID string) error {
	sub, err := s.repo.CurrentSubscription(ctx, userID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	sub.Status = domain.SubCancelled
	sub.CancelledAt = &now
	return s.repo.UpdateSubscription(ctx, sub)
}

// ExpireDue — мерзімі өткендерін белгілеу (фон тапсырмасы).
func (s *Service) ExpireDue(ctx context.Context) (int64, error) {
	return s.repo.ExpireDueSubscriptions(ctx, s.clock.Now())
}

// Entitlement — қолданушының дәл қазіргі лимиттері мен қолданысы.
//
// Жазылым мерзімі өткен болса, қолданушы қызметсіз қалмайды: тегін тарифке
// түседі. Клиенттің «мен неше рет қолдандым» деген сөзіне ешқашан сенбейміз.
func (s *Service) Entitlement(ctx context.Context, userID string) (domain.Entitlement, error) {
	now := s.clock.Now()
	var (
		plan domain.Plan
		sub  *domain.Subscription
	)

	current, err := s.repo.CurrentSubscription(ctx, userID)
	switch {
	case err == nil:
		if current.IsUsable(now) {
			if p, err := s.repo.Plan(ctx, current.PlanID); err == nil {
				plan, sub = p, &current
			}
		}
	case !errors.Is(err, domain.ErrNotFound):
		return domain.Entitlement{}, err
	}

	if plan.ID == "" {
		free, err := s.plans.Default(ctx)
		if err != nil {
			return domain.Entitlement{}, err
		}
		plan = free
	}

	date, month := s.Keys(now)
	usedDay, usedMonth, err := s.repo.Counters(ctx, userID, date, month)
	if err != nil {
		return domain.Entitlement{}, err
	}

	return domain.Entitlement{
		Plan:         plan,
		Subscription: sub,
		DailyLimit:   plan.DailyLimit,
		MonthlyLimit: plan.MonthlyLimit,
		UsedToday:    usedDay,
		UsedMonth:    usedMonth,
		ResetsAt:     s.NextReset(now),
	}, nil
}

// Keys — квота кілттері: күн (YYYY-MM-DD) және ай (YYYY-MM) сервер белдеуінде.
//
// The reset boundary is the server's configured timezone, never the device
// clock: a phone with the wrong date cannot buy itself extra generations.
func (s *Service) Keys(now time.Time) (date string, month string) {
	local := now.In(s.loc)
	return local.Format("2006-01-02"), local.Format("2006-01")
}

// NextReset — келесі қайта жаңару сәті (жергілікті түн ортасы).
func (s *Service) NextReset(now time.Time) time.Time {
	local := now.In(s.loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, 1)
	return next.UTC()
}

// Now — қызметтің уақыты (тестте жалған сағат қойылады).
func (s *Service) Now() time.Time { return s.clock.Now() }

// TodayKeys — ағымдағы күн мен ай кілттері. Барлық қабат осыны пайдаланады,
// сондықтан әкімші панелі мен AI шлюзі әрқашан бір күнді көреді.
func (s *Service) TodayKeys() (string, string) { return s.Keys(s.clock.Now()) }

// Location — квота белдеуі.
func (s *Service) Location() *time.Location { return s.loc }
