package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const planColumns = `id, code, name_kk, name_ru, name_en, name_uz,
	description_kk, description_ru, description_en, description_uz,
	price, currency, daily_message_limit, monthly_message_limit, period_days,
	is_free, is_active, sort_order, created_at, updated_at, archived_at, is_visible`

func scanPlan(row interface{ Scan(...any) error }) (domain.Plan, error) {
	var (
		p                  domain.Plan
		nkk, nru, nen, nuz string
		dkk, dru, den, duz string
		free, active, seen int
		created, updated   int64
		archived           sql.NullInt64
	)
	if err := row.Scan(&p.ID, &p.Code, &nkk, &nru, &nen, &nuz, &dkk, &dru, &den, &duz,
		&p.Price, &p.Currency, &p.DailyLimit, &p.MonthlyLimit, &p.PeriodDays,
		&free, &active, &p.SortOrder, &created, &updated, &archived, &seen); err != nil {
		return domain.Plan{}, err
	}
	p.Name = map[string]string{"kk": nkk, "ru": nru, "en": nen, "uz": nuz}
	p.Description = map[string]string{"kk": dkk, "ru": dru, "en": den, "uz": duz}
	p.IsFree, p.IsActive, p.IsVisible = free == 1, active == 1, seen == 1
	p.CreatedAt, p.UpdatedAt, p.ArchivedAt = timeFrom(created), timeFrom(updated), timePtr(archived)
	return p, nil
}

// Plans — тізім. activeOnly=true болса, тек қосулы әрі мұрағатта емес тарифтер.
func (s *Store) Plans(ctx context.Context, activeOnly bool) ([]domain.Plan, error) {
	where := ``
	if activeOnly {
		where = ` WHERE is_active = 1 AND archived_at IS NULL`
	}
	return s.queryPlans(ctx, where)
}

// ListedPlans — клиентке көрінетін тарифтер (қолданба, лендинг, /api/v1/plans).
func (s *Store) ListedPlans(ctx context.Context) ([]domain.Plan, error) {
	return s.queryPlans(ctx, ` WHERE is_active = 1 AND is_visible = 1 AND archived_at IS NULL`)
}

func (s *Store) queryPlans(ctx context.Context, where string) ([]domain.Plan, error) {
	query := `SELECT ` + planColumns + ` FROM plans` + where + ` ORDER BY sort_order ASC, price ASC`
	rows, err := s.db.Reader().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Plan — идентификатор бойынша.
func (s *Store) Plan(ctx context.Context, id string) (domain.Plan, error) {
	p, err := scanPlan(s.db.Reader().QueryRowContext(ctx, `SELECT `+planColumns+` FROM plans WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, domain.ErrNotFound
	}
	return p, err
}

// PlanByCode — код бойынша (free, standard, pro).
func (s *Store) PlanByCode(ctx context.Context, code string) (domain.Plan, error) {
	p, err := scanPlan(s.db.Reader().QueryRowContext(ctx, `SELECT `+planColumns+` FROM plans WHERE code = ?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, domain.ErrNotFound
	}
	return p, err
}

// CreatePlan — әкімші жаңа тариф қосады.
func (s *Store) CreatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	if p.ID == "" {
		p.ID = traits.NewID()
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO plans (`+planColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Code, p.Name["kk"], p.Name["ru"], p.Name["en"], p.Name["uz"],
		p.Description["kk"], p.Description["ru"], p.Description["en"], p.Description["uz"],
		p.Price, p.Currency, p.DailyLimit, p.MonthlyLimit, p.PeriodDays,
		boolInt(p.IsFree), boolInt(p.IsActive), p.SortOrder, ms(now), ms(now), nil, boolInt(p.IsVisible))
	if isUnique(err) {
		return domain.Plan{}, domain.ErrConflict
	}
	return p, err
}

// UpdatePlan — тарифті өзгерту. Лимитті өзгерту үшін мобильді релиз қажет емес.
func (s *Store) UpdatePlan(ctx context.Context, p domain.Plan) error {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE plans SET code = ?, name_kk = ?, name_ru = ?, name_en = ?, name_uz = ?,
			description_kk = ?, description_ru = ?, description_en = ?, description_uz = ?,
			price = ?, currency = ?, daily_message_limit = ?, monthly_message_limit = ?,
			period_days = ?, is_free = ?, is_active = ?, is_visible = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		p.Code, p.Name["kk"], p.Name["ru"], p.Name["en"], p.Name["uz"],
		p.Description["kk"], p.Description["ru"], p.Description["en"], p.Description["uz"],
		p.Price, p.Currency, p.DailyLimit, p.MonthlyLimit, p.PeriodDays,
		boolInt(p.IsFree), boolInt(p.IsActive), boolInt(p.IsVisible), p.SortOrder, ms(time.Now()), p.ID)
	if isUnique(err) {
		return domain.ErrConflict
	}
	return affected(res, err)
}

// ArchivePlan — жою орнына мұрағаттау (жазылымдар сілтемесі сақталады).
func (s *Store) ArchivePlan(ctx context.Context, id string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE plans SET archived_at = ?, is_active = 0, updated_at = ? WHERE id = ? AND archived_at IS NULL`,
		ms(time.Now()), ms(time.Now()), id)
	return affected(res, err)
}

// PlanUsageCount — тарифті пайдаланушылар саны.
func (s *Store) PlanUsageCount(ctx context.Context, planID string) (int, error) {
	var n int
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM subscriptions WHERE plan_id = ? AND status IN ('active','trial')`, planID).Scan(&n)
	return n, err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
