package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Pricing — модель бағасы (болжамды құн осыдан есептеледі).
type Pricing struct {
	ID            string
	Model         string
	InputPer1M    float64
	OutputPer1M   float64
	Currency      string
	EffectiveFrom time.Time
}

// PricingFor — модельдің қолданыстағы бағасы.
func (s *Store) PricingFor(ctx context.Context, model string, at time.Time) (Pricing, error) {
	var (
		p         Pricing
		effective int64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, model, input_price_per_1m, output_price_per_1m, currency, effective_from
		FROM model_pricing WHERE model = ? AND effective_from <= ?
		ORDER BY effective_from DESC LIMIT 1`, model, ms(at)).
		Scan(&p.ID, &p.Model, &p.InputPer1M, &p.OutputPer1M, &p.Currency, &effective)
	if errors.Is(err, sql.ErrNoRows) {
		return Pricing{}, domain.ErrNotFound
	}
	if err != nil {
		return Pricing{}, err
	}
	p.EffectiveFrom = timeFrom(effective)
	return p, nil
}

// ListPricing — әкімшіге арналған тізім.
func (s *Store) ListPricing(ctx context.Context) ([]Pricing, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, model, input_price_per_1m, output_price_per_1m, currency, effective_from
		FROM model_pricing ORDER BY model, effective_from DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pricing
	for rows.Next() {
		var (
			p         Pricing
			effective int64
		)
		if err := rows.Scan(&p.ID, &p.Model, &p.InputPer1M, &p.OutputPer1M, &p.Currency, &effective); err != nil {
			return nil, err
		}
		p.EffectiveFrom = timeFrom(effective)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SavePricing — жаңа баға кезеңі.
func (s *Store) SavePricing(ctx context.Context, p Pricing) error {
	if p.ID == "" {
		p.ID = traits.NewID()
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO model_pricing (id, model, input_price_per_1m, output_price_per_1m, currency, effective_from, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		p.ID, p.Model, p.InputPer1M, p.OutputPer1M, p.Currency, ms(p.EffectiveFrom), ms(time.Now()))
	return err
}

// ---------------------------------------------------------------- payments

// CreatePayment — төлем әрекеті (demo адаптерінде де жазылады).
func (s *Store) CreatePayment(ctx context.Context, p domain.Payment) (domain.Payment, error) {
	if p.ID == "" {
		p.ID = traits.NewID()
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO payments (id, user_id, plan_id, provider, provider_ref, amount, currency, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.UserID, p.PlanID, p.Provider, p.ProviderRef, p.Amount, p.Currency, p.Status, ms(now), ms(now))
	return p, err
}

// Payment — бір төлем.
func (s *Store) Payment(ctx context.Context, id string) (domain.Payment, error) {
	var p domain.Payment
	var created, updated int64
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, user_id, plan_id, provider, provider_ref, amount, currency, status, created_at, updated_at
		FROM payments WHERE id = ?`, id).
		Scan(&p.ID, &p.UserID, &p.PlanID, &p.Provider, &p.ProviderRef, &p.Amount, &p.Currency,
			&p.Status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Payment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, err
	}
	p.CreatedAt, p.UpdatedAt = timeFrom(created), timeFrom(updated)
	return p, nil
}

// UpdatePaymentStatus — төлем күйі.
func (s *Store) UpdatePaymentStatus(ctx context.Context, id, status, ref string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE payments SET status = ?, provider_ref = CASE WHEN ? <> '' THEN ? ELSE provider_ref END, updated_at = ?
		 WHERE id = ?`, status, ref, ref, ms(time.Now()), id)
	return affected(res, err)
}

// PaymentsByUser — тарих.
func (s *Store) PaymentsByUser(ctx context.Context, userID string, limit int) ([]domain.Payment, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, user_id, plan_id, provider, provider_ref, amount, currency, status, created_at, updated_at
		FROM payments WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Payment
	for rows.Next() {
		var p domain.Payment
		var created, updated int64
		if err := rows.Scan(&p.ID, &p.UserID, &p.PlanID, &p.Provider, &p.ProviderRef, &p.Amount,
			&p.Currency, &p.Status, &created, &updated); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = timeFrom(created), timeFrom(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- settings

// Setting — жүйелік параметр.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return v, err
}

// SetSetting — параметрді жазу.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO system_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, ms(time.Now()))
	return err
}

// SetSettings — бірнеше параметрді бір транзакцияда жазу: не бәрі сақталады, не ешқайсысы.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := ms(time.Now())
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO system_settings (key, value, updated_at) VALUES (?,?,?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key, value, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RecordAppVersion — қолданба нұсқасын белгілеу (аналитика үшін).
func (s *Store) RecordAppVersion(ctx context.Context, platform, version, build string) error {
	if platform == "" || version == "" {
		return nil
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO app_versions (id, platform, version, build, released_at) VALUES (?,?,?,?,?)
		ON CONFLICT (platform, version) DO UPDATE SET build = excluded.build`,
		traits.NewID(), platform, version, build, ms(time.Now()))
	return err
}
