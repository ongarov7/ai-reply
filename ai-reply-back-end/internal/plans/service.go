// Package plans — тарифтер каталогы. Лимиттер тек дерекқорда тұрады.
package plans

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// ErrDefaultPlan — әдепкі тегін тарифті өшіруге не мұрағаттауға болмайды:
// every new account is put on it, so registration would stop working.
var ErrDefaultPlan = fmt.Errorf("%w: the default plan must stay enabled and free", domain.ErrInvalidRequest)

// Service — тарифтермен жұмыс.
type Service struct{ repo *repository.Store }

// New — қызмет.
func New(repo *repository.Store) *Service { return &Service{repo: repo} }

// Active — қосулы тарифтер (көрінбейтіндері де бар).
func (s *Service) Active(ctx context.Context) ([]domain.Plan, error) {
	return s.repo.Plans(ctx, true)
}

// Listed — клиентке көрінетін тарифтер: мобильді қолданба мен лендинг.
//
// Read straight from the database on every call, so a change in the admin
// panel reaches the apps on their next request without a release.
func (s *Service) Listed(ctx context.Context) ([]domain.Plan, error) {
	return s.repo.ListedPlans(ctx)
}

// All — әкімшіге арналған толық тізім.
func (s *Service) All(ctx context.Context) ([]domain.Plan, error) {
	return s.repo.Plans(ctx, false)
}

// Get — id бойынша.
func (s *Service) Get(ctx context.Context, id string) (domain.Plan, error) {
	return s.repo.Plan(ctx, id)
}

// Default — жаңа қолданушыға берілетін тегін тариф.
func (s *Service) Default(ctx context.Context) (domain.Plan, error) {
	code, err := s.repo.Setting(ctx, "default_plan_code")
	if err != nil || code == "" {
		code = "free"
	}
	plan, err := s.repo.PlanByCode(ctx, code)
	if err == nil {
		return plan, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Plan{}, err
	}
	// Қор нұсқасы: бірінші белсенді тегін тариф.
	all, err := s.repo.Plans(ctx, true)
	if err != nil {
		return domain.Plan{}, err
	}
	for _, p := range all {
		if p.IsFree {
			return p, nil
		}
	}
	return domain.Plan{}, domain.ErrNotFound
}

// Create — валидациямен жасау.
func (s *Service) Create(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	if err := validate(&p); err != nil {
		return domain.Plan{}, err
	}
	return s.repo.CreatePlan(ctx, p)
}

// Update — өзгерту; лимит өзгерісі бірден бүкіл клиентке тарайды.
func (s *Service) Update(ctx context.Context, p domain.Plan) error {
	if err := validate(&p); err != nil {
		return err
	}
	if isDefault, err := s.isDefault(ctx, p.ID); err != nil {
		return err
	} else if isDefault && (!p.IsActive || !p.IsFree) {
		return ErrDefaultPlan
	}
	return s.repo.UpdatePlan(ctx, p)
}

// Archive — мұрағаттау (жою емес: тарих сақталады).
func (s *Service) Archive(ctx context.Context, id string) error {
	if isDefault, err := s.isDefault(ctx, id); err != nil {
		return err
	} else if isDefault {
		return ErrDefaultPlan
	}
	return s.repo.ArchivePlan(ctx, id)
}

// IsDefault — бұл тариф жаңа қолданушыларға беріле ме.
func (s *Service) IsDefault(ctx context.Context, id string) (bool, error) {
	return s.isDefault(ctx, id)
}

func (s *Service) isDefault(ctx context.Context, id string) (bool, error) {
	plan, err := s.Default(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return plan.ID == id, nil
}

// UsageCount — тарифтегі белсенді жазылым саны.
func (s *Service) UsageCount(ctx context.Context, id string) (int, error) {
	return s.repo.PlanUsageCount(ctx, id)
}

func validate(p *domain.Plan) error {
	p.Code = strings.ToLower(strings.TrimSpace(p.Code))
	if p.Code == "" || len(p.Code) > 40 {
		return domain.ErrInvalidRequest
	}
	if p.Name == nil {
		p.Name = map[string]string{}
	}
	if p.Description == nil {
		p.Description = map[string]string{}
	}
	if strings.TrimSpace(p.Name["en"]) == "" {
		return domain.ErrInvalidRequest
	}
	for _, l := range domain.Locales {
		if strings.TrimSpace(p.Name[l]) == "" {
			p.Name[l] = p.Name["en"]
		}
	}
	if p.Currency == "" {
		p.Currency = "KZT"
	}
	if p.DailyLimit < 0 || p.DailyLimit > 100000 {
		return domain.ErrInvalidRequest
	}
	if p.MonthlyLimit < 0 {
		return domain.ErrInvalidRequest
	}
	if p.Price < 0 {
		return domain.ErrInvalidRequest
	}
	if p.IsFree {
		p.Price = 0
		p.PeriodDays = 0
	} else if p.PeriodDays <= 0 {
		p.PeriodDays = 30
	}
	return nil
}
