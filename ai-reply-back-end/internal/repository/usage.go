package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// ReserveQuota — квотаны АЛДЫН АЛА бір атомарлы транзакцияда брондайды.
//
// The counter is incremented before the provider is called, guarded by
// `used < limit` inside the UPDATE itself. Two parallel requests on the last
// remaining generation therefore cannot both succeed: the second UPDATE
// matches no row and comes back as a limit error. A provider failure refunds
// the reservation, so a user is only ever charged for a generation they got.
func (s *Store) ReserveQuota(ctx context.Context, userID, date, month string, dailyLimit, monthlyLimit int) error {
	if dailyLimit <= 0 {
		return domain.ErrDailyLimit
	}
	now := ms(time.Now())
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_daily (user_id, usage_date, used, updated_at)
			VALUES (?, ?, 0, ?) ON CONFLICT (user_id, usage_date) DO NOTHING`,
			userID, date, now); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE usage_daily SET used = used + 1, updated_at = ?
			WHERE user_id = ? AND usage_date = ? AND used < ?`, now, userID, date, dailyLimit)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrDailyLimit
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_monthly (user_id, usage_month, used, updated_at)
			VALUES (?, ?, 0, ?) ON CONFLICT (user_id, usage_month) DO NOTHING`,
			userID, month, now); err != nil {
			return err
		}
		if monthlyLimit > 0 {
			res, err = tx.ExecContext(ctx, `
				UPDATE usage_monthly SET used = used + 1, updated_at = ?
				WHERE user_id = ? AND usage_month = ? AND used < ?`, now, userID, month, monthlyLimit)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return domain.ErrMonthlyLimit // транзакция кері қайтарылады, күндік те есептелмейді
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE usage_monthly SET used = used + 1, updated_at = ? WHERE user_id = ? AND usage_month = ?`,
			now, userID, month)
		return err
	})
}

// RefundQuota — провайдер қатесінде бронды қайтарады (қолданушы алмаған жауап үшін төлемейді).
func (s *Store) RefundQuota(ctx context.Context, userID, date, month string) error {
	now := ms(time.Now())
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE usage_daily SET used = used - 1, updated_at = ?
			WHERE user_id = ? AND usage_date = ? AND used > 0`, now, userID, date); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE usage_monthly SET used = used - 1, updated_at = ?
			WHERE user_id = ? AND usage_month = ? AND used > 0`, now, userID, month)
		return err
	})
}

// AddTokens — сәтті сұраныстың токен/құн метадерегі.
func (s *Store) AddTokens(ctx context.Context, userID, date, month string, in, out int, costMicros int64) error {
	now := ms(time.Now())
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE usage_daily SET input_tokens = input_tokens + ?, output_tokens = output_tokens + ?,
			       cost_micros = cost_micros + ?, updated_at = ?
			WHERE user_id = ? AND usage_date = ?`, in, out, costMicros, now, userID, date); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE usage_monthly SET input_tokens = input_tokens + ?, output_tokens = output_tokens + ?,
			       cost_micros = cost_micros + ?, updated_at = ?
			WHERE user_id = ? AND usage_month = ?`, in, out, costMicros, now, userID, month)
		return err
	})
}

// Counters — бүгінгі және айлық қолданыс.
func (s *Store) Counters(ctx context.Context, userID, date, month string) (day int, mon int, err error) {
	err = s.db.Reader().QueryRowContext(ctx,
		`SELECT COALESCE((SELECT used FROM usage_daily WHERE user_id = ? AND usage_date = ?), 0),
		        COALESCE((SELECT used FROM usage_monthly WHERE user_id = ? AND usage_month = ?), 0)`,
		userID, date, userID, month).Scan(&day, &mon)
	return
}

// ResetDailyQuota — әкімшінің «бүгінгі квотаны қалпына келтіру» әрекеті.
func (s *Store) ResetDailyQuota(ctx context.Context, userID, date string) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`UPDATE usage_daily SET used = 0, updated_at = ? WHERE user_id = ? AND usage_date = ?`,
		ms(time.Now()), userID, date)
	return err
}

// InsertUsageEvent — тек метадерек жазылады (мәтін ешқашан емес).
func (s *Store) InsertUsageEvent(ctx context.Context, e domain.UsageEvent) error {
	if e.ID == "" {
		e.ID = traits.NewID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Mode == "" {
		e.Mode = "reply"
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO ai_usage_events (id, user_id, device_id, plan_id, model, status, error_code,
			input_tokens, output_tokens, total_tokens, cost_micros, latency_ms, provider_ms,
			platform, app_version, language, source_chars, mode, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.UserID, e.DeviceID, e.PlanID, e.Model, e.Status, e.ErrorCode,
		e.InputTokens, e.OutputTokens, e.TotalTokens, e.CostMicros, e.LatencyMS, e.ProviderMS,
		e.Platform, e.AppVersion, e.Language, e.SourceChars, e.Mode, ms(e.CreatedAt))
	return err
}

// ---------------------------------------------------------------- analytics

// Stats — басқару тақтасының жиынтық сандары.
type Stats struct {
	TotalUsers     int
	NewUsersToday  int
	NewUsersMonth  int
	ActiveUsers30d int
	PaidUsers      int
	FreeUsers      int
	RequestsToday  int
	RequestsMonth  int
	InputTokens    int
	OutputTokens   int
	TotalTokens    int
	CostMicros     int64
	Succeeded      int
	Failed         int
	AvgLatencyMS   int
	IOSUsers       int
	AndroidUsers   int
}

// Stats — көрсетілген кезең бойынша метрика (мазмұнсыз).
func (s *Store) Stats(ctx context.Context, from, to time.Time, today string) (Stats, error) {
	var st Stats
	r := s.db.Reader()

	if err := r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&st.TotalUsers); err != nil {
		return st, err
	}
	dayStart := startOfDayMillis(time.Now())
	if err := r.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE created_at >= ? AND deleted_at IS NULL`, dayStart).Scan(&st.NewUsersToday); err != nil {
		return st, err
	}
	if err := r.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE created_at >= ? AND deleted_at IS NULL`,
		ms(time.Now().UTC().AddDate(0, 0, -30))).Scan(&st.NewUsersMonth); err != nil {
		return st, err
	}
	if err := r.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE last_active_at >= ? AND deleted_at IS NULL`,
		ms(time.Now().UTC().AddDate(0, 0, -30))).Scan(&st.ActiveUsers30d); err != nil {
		return st, err
	}
	if err := r.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT s.user_id) FROM subscriptions s
		JOIN plans p ON p.id = s.plan_id
		WHERE s.status IN ('active','trial') AND p.is_free = 0`).Scan(&st.PaidUsers); err != nil {
		return st, err
	}
	st.FreeUsers = st.TotalUsers - st.PaidUsers
	if err := r.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(used),0) FROM usage_daily WHERE usage_date = ?`, today).Scan(&st.RequestsToday); err != nil {
		return st, err
	}
	if err := r.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		       COALESCE(SUM(total_tokens),0), COALESCE(SUM(cost_micros),0),
		       COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END),0),
		       COALESCE(CAST(AVG(latency_ms) AS INTEGER),0)
		FROM ai_usage_events WHERE created_at BETWEEN ? AND ?`, ms(from), ms(to)).
		Scan(&st.RequestsMonth, &st.InputTokens, &st.OutputTokens, &st.TotalTokens, &st.CostMicros,
			&st.Succeeded, &st.Failed, &st.AvgLatencyMS); err != nil {
		return st, err
	}
	if err := r.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN platform = 'ios' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN platform = 'android' THEN 1 ELSE 0 END),0)
		 FROM users WHERE deleted_at IS NULL`).Scan(&st.IOSUsers, &st.AndroidUsers); err != nil {
		return st, err
	}
	return st, nil
}

// Point — график нүктесі.
type Point struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// SeriesRegistrations — тіркелулер динамикасы.
func (s *Store) SeriesRegistrations(ctx context.Context, from, to time.Time) ([]Point, error) {
	return s.series(ctx, `
		SELECT strftime('%Y-%m-%d', created_at/1000, 'unixepoch'), COUNT(*)
		FROM users WHERE created_at BETWEEN ? AND ? AND deleted_at IS NULL
		GROUP BY 1 ORDER BY 1`, from, to)
}

// SeriesGenerations — күндік генерация саны.
func (s *Store) SeriesGenerations(ctx context.Context, from, to time.Time) ([]Point, error) {
	return s.series(ctx, `
		SELECT strftime('%Y-%m-%d', created_at/1000, 'unixepoch'), COUNT(*)
		FROM ai_usage_events WHERE created_at BETWEEN ? AND ? AND status = 'success'
		GROUP BY 1 ORDER BY 1`, from, to)
}

// SeriesTokens — токен шығыны.
func (s *Store) SeriesTokens(ctx context.Context, from, to time.Time) ([]Point, error) {
	return s.series(ctx, `
		SELECT strftime('%Y-%m-%d', created_at/1000, 'unixepoch'), COALESCE(SUM(total_tokens),0)
		FROM ai_usage_events WHERE created_at BETWEEN ? AND ?
		GROUP BY 1 ORDER BY 1`, from, to)
}

// SeriesCost — болжамды құн (АҚШ доллары).
func (s *Store) SeriesCost(ctx context.Context, from, to time.Time) ([]Point, error) {
	points, err := s.series(ctx, `
		SELECT strftime('%Y-%m-%d', created_at/1000, 'unixepoch'), COALESCE(SUM(cost_micros),0)
		FROM ai_usage_events WHERE created_at BETWEEN ? AND ?
		GROUP BY 1 ORDER BY 1`, from, to)
	for i := range points {
		points[i].Value = points[i].Value / 1_000_000
	}
	return points, err
}

// SeriesActiveUsers — тәуліктік белсенді қолданушылар.
func (s *Store) SeriesActiveUsers(ctx context.Context, from, to time.Time) ([]Point, error) {
	return s.series(ctx, `
		SELECT strftime('%Y-%m-%d', created_at/1000, 'unixepoch'), COUNT(DISTINCT user_id)
		FROM ai_usage_events WHERE created_at BETWEEN ? AND ?
		GROUP BY 1 ORDER BY 1`, from, to)
}

// DistributionPlans — тарифтер бойынша үлес.
func (s *Store) DistributionPlans(ctx context.Context) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT p.code, COUNT(*) FROM subscriptions s
		JOIN plans p ON p.id = s.plan_id
		WHERE s.status IN ('active','trial')
		GROUP BY p.code ORDER BY 2 DESC`)
	return collectPoints(rows, err)
}

// DistributionPlatforms — платформалар бойынша үлес.
func (s *Store) DistributionPlatforms(ctx context.Context) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT CASE WHEN platform = '' THEN 'unknown' ELSE platform END, COUNT(*)
		FROM users WHERE deleted_at IS NULL GROUP BY 1 ORDER BY 2 DESC`)
	return collectPoints(rows, err)
}

// AppVersionBreakdown — қолданбаның нұсқалары.
func (s *Store) AppVersionBreakdown(ctx context.Context) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT CASE WHEN app_version = '' THEN 'unknown' ELSE platform || ' ' || app_version END, COUNT(*)
		FROM users WHERE deleted_at IS NULL GROUP BY 1 ORDER BY 2 DESC LIMIT 20`)
	return collectPoints(rows, err)
}

// TopUsersByCost — шығыны жоғары қолданушылар (UUID ғана).
func (s *Store) TopUsersByCost(ctx context.Context, from, to time.Time, limit int) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT user_id, COALESCE(SUM(cost_micros),0) c FROM ai_usage_events
		WHERE created_at BETWEEN ? AND ? GROUP BY user_id ORDER BY c DESC LIMIT ?`,
		ms(from), ms(to), limit)
	points, err := collectPoints(rows, err)
	for i := range points {
		points[i].Value = points[i].Value / 1_000_000
	}
	return points, err
}

// ErrorBreakdown — қате кодтары бойынша.
func (s *Store) ErrorBreakdown(ctx context.Context, from, to time.Time) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT error_code, COUNT(*) FROM ai_usage_events
		WHERE created_at BETWEEN ? AND ? AND status <> 'success' AND error_code <> ''
		GROUP BY error_code ORDER BY 2 DESC`, ms(from), ms(to))
	return collectPoints(rows, err)
}

// UserEvents — бір қолданушының соңғы сұраныс метадерегі.
func (s *Store) UserEvents(ctx context.Context, userID string, limit int) ([]domain.UsageEvent, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, user_id, device_id, plan_id, model, status, error_code, input_tokens, output_tokens,
		       total_tokens, cost_micros, latency_ms, provider_ms, platform, app_version, language,
		       source_chars, mode, created_at
		FROM ai_usage_events WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UsageEvent
	for rows.Next() {
		var e domain.UsageEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.UserID, &e.DeviceID, &e.PlanID, &e.Model, &e.Status, &e.ErrorCode,
			&e.InputTokens, &e.OutputTokens, &e.TotalTokens, &e.CostMicros, &e.LatencyMS, &e.ProviderMS,
			&e.Platform, &e.AppVersion, &e.Language, &e.SourceChars, &e.Mode, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = timeFrom(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) series(ctx context.Context, query string, from, to time.Time) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, query, ms(from), ms(to))
	return collectPoints(rows, err)
}

func collectPoints(rows *sql.Rows, err error) ([]Point, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Label, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func startOfDayMillis(t time.Time) int64 {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).UnixMilli()
}
