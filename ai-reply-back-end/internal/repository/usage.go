package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// AddTokens — сәтті сұраныстың токен/құн метадерегі.
//
// Upsert, not UPDATE: a polish request reserves no quota, so the day's row
// may not exist yet, and its tokens must still be counted. `used` and
// `extra_used` are never touched here — only ReserveGeneration and
// RefundGeneration move them.
func (s *Store) AddTokens(ctx context.Context, userID, date, month string, in, out int, costMicros int64) error {
	now := ms(time.Now())
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_daily (user_id, usage_date, used, input_tokens, output_tokens, cost_micros, updated_at)
			VALUES (?, ?, 0, ?, ?, ?, ?)
			ON CONFLICT (user_id, usage_date) DO UPDATE SET
				input_tokens = input_tokens + excluded.input_tokens,
				output_tokens = output_tokens + excluded.output_tokens,
				cost_micros = cost_micros + excluded.cost_micros,
				updated_at = excluded.updated_at`,
			userID, date, in, out, costMicros, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO usage_monthly (user_id, usage_month, used, input_tokens, output_tokens, cost_micros, updated_at)
			VALUES (?, ?, 0, ?, ?, ?, ?)
			ON CONFLICT (user_id, usage_month) DO UPDATE SET
				input_tokens = input_tokens + excluded.input_tokens,
				output_tokens = output_tokens + excluded.output_tokens,
				cost_micros = cost_micros + excluded.cost_micros,
				updated_at = excluded.updated_at`,
			userID, month, in, out, costMicros, now)
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
			platform, app_version, language, source_chars, mode, prompt_version, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.UserID, e.DeviceID, e.PlanID, e.Model, e.Status, e.ErrorCode,
		e.InputTokens, e.OutputTokens, e.TotalTokens, e.CostMicros, e.LatencyMS, e.ProviderMS,
		e.Platform, e.AppVersion, e.Language, e.SourceChars, e.Mode, e.PromptVersion, ms(e.CreatedAt))
	return err
}

// UsageEventsSince — қолданушының осы режимдегі since-тен бергі сұраныстары (idx_ai_events_user).
func (s *Store) UsageEventsSince(ctx context.Context, userID, mode string, since time.Time) (int, error) {
	var n int
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND created_at >= ? AND mode = ?`,
		userID, ms(since), mode).Scan(&n)
	return n, err
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
	// Refused — лимитке тірелген сұраныстар: олар сәтсіз генерация емес.
	Refused      int
	AvgLatencyMS int
	IOSUsers     int
	AndroidUsers int
}

// Stats — көрсетілген кезең бойынша метрика (мазмұнсыз).
//
// today is the usage_daily key and dayStart the same day's local midnight,
// both in the app timezone, so every "today" tile counts the same day the
// quota does.
func (s *Store) Stats(ctx context.Context, from, to time.Time, today string, dayStart time.Time) (Stats, error) {
	var st Stats
	r := s.db.Reader()

	if err := r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&st.TotalUsers); err != nil {
		return st, err
	}
	if err := r.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE created_at >= ? AND deleted_at IS NULL`, ms(dayStart)).Scan(&st.NewUsersToday); err != nil {
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
		`SELECT COALESCE(SUM(used + extra_used),0) FROM usage_daily WHERE usage_date = ?`, today).Scan(&st.RequestsToday); err != nil {
		return st, err
	}
	// Polish (нұсқауды түзету) пен transcribe (дауыс → мәтін) — генерация емес:
	// санға да, кідіріске де кірмейді, бірақ токен мен құн шынайы шығын
	// болғандықтан есептеледі. Лимитке тірелген сұраныс провайдерге жетпейді:
	// ол сәтсіз генерация да емес, кідіріске де кірмейді (Refused).
	if err := r.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN mode NOT IN ('polish','transcribe') THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		       COALESCE(SUM(total_tokens),0), COALESCE(SUM(cost_micros),0),
		       COALESCE(SUM(CASE WHEN mode NOT IN ('polish','transcribe') AND status = 'success' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN mode NOT IN ('polish','transcribe') AND status <> 'success'
		                          AND error_code NOT IN (`+limitCodes+`) THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN mode NOT IN ('polish','transcribe') AND status <> 'success'
		                          AND error_code IN (`+limitCodes+`) THEN 1 ELSE 0 END),0),
		       COALESCE(CAST(AVG(CASE WHEN mode NOT IN ('polish','transcribe')
		                              AND NOT (status <> 'success' AND error_code IN (`+limitCodes+`))
		                              THEN latency_ms END) AS INTEGER),0)
		FROM ai_usage_events WHERE created_at BETWEEN ? AND ?`, ms(from), ms(to)).
		Scan(&st.RequestsMonth, &st.InputTokens, &st.OutputTokens, &st.TotalTokens, &st.CostMicros,
			&st.Succeeded, &st.Failed, &st.Refused, &st.AvgLatencyMS); err != nil {
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

// SeriesGenerations — күндік генерация саны (жауап пен жаңа хабарлама; polish пен transcribe емес).
func (s *Store) SeriesGenerations(ctx context.Context, from, to time.Time) ([]Point, error) {
	return s.series(ctx, `
		SELECT strftime('%Y-%m-%d', created_at/1000, 'unixepoch'), COUNT(*)
		FROM ai_usage_events
		WHERE created_at BETWEEN ? AND ? AND status = 'success' AND mode NOT IN ('polish','transcribe')
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
		       source_chars, mode, prompt_version, created_at
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
			&e.Platform, &e.AppVersion, &e.Language, &e.SourceChars, &e.Mode, &e.PromptVersion, &created); err != nil {
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

// limitCodes — квота бас тартуларының кодтары (ai_usage_events.error_code).
const limitCodes = `'DAILY_LIMIT_REACHED','MONTHLY_LIMIT_REACHED'`
