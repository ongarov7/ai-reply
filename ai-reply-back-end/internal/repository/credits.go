package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// querier — *sql.DB мен *sql.Tx-тің оқу бөлігі.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ReserveInput — бір генерацияның брон сұранысы.
type ReserveInput struct {
	UserID string
	// Key — клиенттің Idempotency-Key тақырыбы ("" — кілтсіз, бұрынғыдай).
	Key  string
	Mode string
	// Date, Month — usage_daily және usage_monthly кілттері (сервер белдеуінде).
	Date  string
	Month string
	// DailyLimit, MonthlyLimit — тариф шектері (MonthlyLimit 0 — айлық шек жоқ).
	DailyLimit   int
	MonthlyLimit int
	Now          time.Time
}

// Reservation — брон нәтижесі.
type Reservation struct {
	ID string
	// Bucket — генерацияны не төледі: daily | pass | welcome | admin | pack.
	Bucket  string
	GrantID string
	// Replay — кілт бұрын сәтті жұмсалған: бұл тегін қайта генерация, ештеңе есептелмеді.
	Replay bool
	// UsedToday, UsedMonth — тарифтің санағыштары осы броннан кейін.
	UsedToday int
	UsedMonth int
	// Balance — тарифтен тыс қалдық осы броннан кейін (сол транзакцияда оқылған).
	Balance domain.CreditBalance
}

// grantOrder — кредиттерді тұтыну реті: pass (ертерек бітетіні), бонус, әкімші, пакет (ескісі).
const grantOrder = `CASE kind WHEN 'pass' THEN 0 WHEN 'welcome' THEN 1 WHEN 'admin' THEN 2 ELSE 3 END,
	expires_at, created_at, id`

// ReserveGeneration — генерацияны провайдерге дейін бір атомарлы транзакцияда брондайды.
//
// The buckets are tried in order: the plan's daily allowance (closed by the
// daily limit or by the plan's monthly cap), then an active pass, the welcome
// bonus, admin grants and packs, oldest first. Every check and decrement runs
// inside one BEGIN IMMEDIATE transaction on the single writer, each guarded
// in the UPDATE itself, so parallel requests can never take the same unit
// and no balance goes below zero. With nothing left the error is
// ErrDailyLimit, or ErrMonthlyLimit when the monthly cap closed the plan.
//
// With a key: a key that is still running answers ConflictField
// (idempotency_key, in_progress); a key that already paid for a generation is
// not charged again — within domain.IdempotencyReplayWindow it may generate
// once more for free (Replay), after that ConflictField (idempotency_key,
// used); a refunded key is charged again as a new reservation.
func (s *Store) ReserveGeneration(ctx context.Context, in ReserveInput) (Reservation, error) {
	var out Reservation
	now := ms(in.Now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if in.Key != "" {
			replay, err := claimKey(ctx, tx, in, &out)
			if err != nil || replay {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_daily (user_id, usage_date, used, updated_at)
			VALUES (?, ?, 0, ?) ON CONFLICT (user_id, usage_date) DO NOTHING`,
			in.UserID, in.Date, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_monthly (user_id, usage_month, used, updated_at)
			VALUES (?, ?, 0, ?) ON CONFLICT (user_id, usage_month) DO NOTHING`,
			in.UserID, in.Month, now); err != nil {
			return err
		}
		day, mon, err := counters(ctx, tx, in.UserID, in.Date, in.Month)
		if err != nil {
			return err
		}

		var closed error
		switch {
		case in.DailyLimit <= 0 || day >= in.DailyLimit:
			closed = domain.ErrDailyLimit
		case in.MonthlyLimit > 0 && mon >= in.MonthlyLimit:
			closed = domain.ErrMonthlyLimit
		}

		out.ID = traits.NewID()
		if closed == nil {
			if err := tx.QueryRowContext(ctx, `
				UPDATE usage_daily SET used = used + 1, updated_at = ?
				WHERE user_id = ? AND usage_date = ? AND used < ? RETURNING used`,
				now, in.UserID, in.Date, in.DailyLimit).Scan(&day); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return domain.ErrDailyLimit
				}
				return err
			}
			guard, args := "", []any{now, in.UserID, in.Month}
			if in.MonthlyLimit > 0 {
				guard, args = " AND used < ?", append(args, in.MonthlyLimit)
			}
			if err := tx.QueryRowContext(ctx, `
				UPDATE usage_monthly SET used = used + 1, updated_at = ?
				WHERE user_id = ? AND usage_month = ?`+guard+` RETURNING used`, args...).Scan(&mon); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return domain.ErrMonthlyLimit // транзакция кері қайтарылады, күндік те есептелмейді
				}
				return err
			}
			out.Bucket = domain.BucketDaily
		} else {
			var kind string
			err := tx.QueryRowContext(ctx, `
				UPDATE credit_grants SET units_remaining = units_remaining - 1, updated_at = ?
				WHERE id = (SELECT id FROM credit_grants
				            WHERE user_id = ? AND status = 'active' AND units_remaining > 0
				              AND kind IN ('pass','welcome','admin','pack')
				              AND starts_at <= ? AND (expires_at IS NULL OR expires_at > ?)
				            ORDER BY `+grantOrder+` LIMIT 1)
				  AND units_remaining > 0
				RETURNING id, kind`, now, in.UserID, now, now).Scan(&out.GrantID, &kind)
			if errors.Is(err, sql.ErrNoRows) {
				return closed
			}
			if err != nil {
				return err
			}
			out.Bucket = kind
			if err := insertLedger(ctx, tx, domain.LedgerEntry{
				UserID: in.UserID, GrantID: out.GrantID, Entry: domain.LedgerConsume, Kind: kind, Delta: -1,
				CreatedAt: in.Now,
			}, out.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE usage_daily SET extra_used = extra_used + 1, updated_at = ? WHERE user_id = ? AND usage_date = ?`,
				now, in.UserID, in.Date); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE usage_monthly SET extra_used = extra_used + 1, updated_at = ? WHERE user_id = ? AND usage_month = ?`,
				now, in.UserID, in.Month); err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_reservations (id, user_id, idempotency_key, mode, bucket, grant_id,
			                                usage_date, usage_month, status, replays, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,0,?,?)`,
			out.ID, in.UserID, nullText(in.Key), in.Mode, out.Bucket, nullText(out.GrantID),
			in.Date, in.Month, domain.ReservationReserved, now, now); err != nil {
			return err
		}
		out.UsedToday, out.UsedMonth = day, mon
		out.Balance, err = creditBalance(ctx, tx, in.UserID, in.Now)
		return err
	})
	if err != nil {
		return Reservation{}, err
	}
	return out, nil
}

// claimKey — кілттің бұрынғы броны. true — бұл қайталау (out толтырылды), жаңа брон керек емес.
func claimKey(ctx context.Context, tx *sql.Tx, in ReserveInput, out *Reservation) (bool, error) {
	var (
		id, status, mode, bucket string
		grant                    sql.NullString
		created                  int64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, status, mode, bucket, grant_id, created_at FROM usage_reservations
		WHERE user_id = ? AND idempotency_key = ?`, in.UserID, in.Key).
		Scan(&id, &status, &mode, &bucket, &grant, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch status {
	case domain.ReservationReserved:
		return true, domain.ConflictField("idempotency_key", "in_progress")
	case domain.ReservationRefunded:
		// The first attempt was given back: the key pays again, as a new row
		// (the ledger keeps the old row's consume and refund).
		_, err := tx.ExecContext(ctx, `DELETE FROM usage_reservations WHERE id = ?`, id)
		return false, err
	}
	if mode != in.Mode {
		return true, domain.ConflictField("idempotency_key", "used")
	}
	now := ms(in.Now)
	res, err := tx.ExecContext(ctx, `
		UPDATE usage_reservations SET replays = replays + 1, updated_at = ?
		WHERE id = ? AND status = 'committed' AND replays < ? AND created_at >= ?`,
		now, id, domain.IdempotencyMaxReplays, ms(in.Now.Add(-domain.IdempotencyReplayWindow)))
	if err != nil {
		return true, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return true, err
		}
		return true, domain.ConflictField("idempotency_key", "used")
	}
	day, mon, err := counters(ctx, tx, in.UserID, in.Date, in.Month)
	if err != nil {
		return true, err
	}
	balance, err := creditBalance(ctx, tx, in.UserID, in.Now)
	if err != nil {
		return true, err
	}
	*out = Reservation{ID: id, Bucket: bucket, GrantID: grant.String, Replay: true,
		UsedToday: day, UsedMonth: mon, Balance: balance}
	return true, nil
}

// counters — тарифтің бүгінгі және айлық санағышы (транзакция ішінде).
func counters(ctx context.Context, tx *sql.Tx, userID, date, month string) (day, mon int, err error) {
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE((SELECT used FROM usage_daily WHERE user_id = ? AND usage_date = ?), 0),
		        COALESCE((SELECT used FROM usage_monthly WHERE user_id = ? AND usage_month = ?), 0)`,
		userID, date, userID, month).Scan(&day, &mon)
	return
}

// CommitGeneration — жауап жетті: брон «committed» болады. false — брон бұдан былай күтілмеген.
func (s *Store) CommitGeneration(ctx context.Context, id string, now time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE usage_reservations SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		domain.ReservationCommitted, ms(now), id, domain.ReservationReserved)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// RefundGeneration — жауап жоқ: бронды төлеген бөлікке қайтарады (бір рет қана).
//
// Only a reservation that is still reserved is refunded, so a provider error
// and the stale sweep racing for the same row give one refund between them.
// A unit goes back to its grant only while the grant is active: a revoked
// grant stays at zero. false: nothing was refunded.
func (s *Store) RefundGeneration(ctx context.Context, id string, now time.Time) (bool, error) {
	refunded := false
	at := ms(now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var (
			userID, bucket, date, month string
			grant                       sql.NullString
		)
		err := tx.QueryRowContext(ctx, `
			UPDATE usage_reservations SET status = ?, updated_at = ?
			WHERE id = ? AND status = ?
			RETURNING user_id, bucket, grant_id, usage_date, usage_month`,
			domain.ReservationRefunded, at, id, domain.ReservationReserved).
			Scan(&userID, &bucket, &grant, &date, &month)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		refunded = true
		column := "extra_used"
		if bucket == domain.BucketDaily {
			column = "used"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE usage_daily SET `+column+` = `+column+` - 1, updated_at = ?
			WHERE user_id = ? AND usage_date = ? AND `+column+` > 0`, at, userID, date); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE usage_monthly SET `+column+` = `+column+` - 1, updated_at = ?
			WHERE user_id = ? AND usage_month = ? AND `+column+` > 0`, at, userID, month); err != nil {
			return err
		}
		if bucket == domain.BucketDaily || !grant.Valid {
			return nil
		}
		var kind string
		err = tx.QueryRowContext(ctx, `
			UPDATE credit_grants SET units_remaining = units_remaining + 1, updated_at = ?
			WHERE id = ? AND status = 'active' AND units_remaining < units_total RETURNING kind`,
			at, grant.String).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return insertLedger(ctx, tx, domain.LedgerEntry{
			UserID: userID, GrantID: grant.String, Entry: domain.LedgerRefund, Kind: kind, Delta: 1, CreatedAt: now,
		}, id)
	})
	return refunded, err
}

// ReleaseReplay — тегін қайта генерация сәтсіз болды: кілт оны тағы бір рет бере алады.
func (s *Store) ReleaseReplay(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE usage_reservations SET replays = replays - 1, updated_at = ? WHERE id = ? AND replays > 0`,
		ms(now), id)
	return err
}

// StaleReservations — before-тан бері «reserved» күйінде қалған брондар (ескісі бірінші).
func (s *Store) StaleReservations(ctx context.Context, before time.Time, limit int) ([]string, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id FROM usage_reservations WHERE status = 'reserved' AND created_at < ?
		ORDER BY created_at LIMIT ?`, ms(before), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeleteOldUsageReservations — cutoff-тан ескі аяқталған брондар (журнал мен кредиттер қалады).
func (s *Store) DeleteOldUsageReservations(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM usage_reservations WHERE rowid IN (
		SELECT rowid FROM usage_reservations WHERE status IN ('committed','refunded') AND created_at < ? LIMIT ?)`,
		cutoff, batch)
}

// ---------------------------------------------------------------- balance

// CreditBalance — тарифтен тыс қалдық (now сәтінде).
func (s *Store) CreditBalance(ctx context.Context, userID string, now time.Time) (domain.CreditBalance, error) {
	return creditBalance(ctx, s.db.Reader(), userID, now)
}

func creditBalance(ctx context.Context, q querier, userID string, now time.Time) (domain.CreditBalance, error) {
	at := ms(now)
	rows, err := q.QueryContext(ctx, `
		SELECT kind, status, units_total, units_remaining, starts_at, expires_at FROM credit_grants
		WHERE user_id = ? AND (kind = 'welcome'
		      OR (status = 'active' AND (units_remaining > 0 OR kind = 'pass')
		          AND (expires_at IS NULL OR expires_at > ?)))`, userID, at)
	if err != nil {
		return domain.CreditBalance{}, err
	}
	defer rows.Close()
	var b domain.CreditBalance
	for rows.Next() {
		var (
			kind, status     string
			total, remaining int
			starts           int64
			expires          sql.NullInt64
		)
		if err := rows.Scan(&kind, &status, &total, &remaining, &starts, &expires); err != nil {
			return domain.CreditBalance{}, err
		}
		usable := status == domain.GrantActive && starts <= at && (!expires.Valid || expires.Int64 > at)
		switch kind {
		case domain.GrantWelcome:
			b.HasWelcome = true
			b.WelcomeTotal += total
			if usable {
				b.WelcomeRemaining += remaining
			}
		case domain.GrantAdmin:
			if usable {
				b.Admin += remaining
			}
		case domain.GrantPack:
			if usable {
				b.Packs += remaining
			}
		case domain.GrantPass:
			if usable {
				b.PassActive = true
				b.PassRemaining += remaining
				if end := timeFrom(expires.Int64); b.PassExpiresAt == nil || end.After(*b.PassExpiresAt) {
					b.PassExpiresAt = &end
				}
			}
		}
	}
	return b, rows.Err()
}

// ---------------------------------------------------------------- grants

// HasWelcomeGrant — тіркелгіде сәлемдесу бонусы бар ма (қайтарылғаны да есептеледі).
func (s *Store) HasWelcomeGrant(ctx context.Context, userID string) (bool, error) {
	var n int
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM credit_grants WHERE user_id = ? AND kind = 'welcome'`, userID).Scan(&n)
	return n > 0, err
}

// InsertWelcomeGrant — бонус және журналдағы «grant» бір транзакцияда; тіркелгіге бір рет қана.
//
// The partial UNIQUE index makes it once per account no matter how many
// sign-ins, platforms or lazy repairs race for it. false: the account already
// had one and nothing was written.
func (s *Store) InsertWelcomeGrant(ctx context.Context, userID string, units int, now time.Time) (bool, error) {
	created := false
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		g := domain.CreditGrant{ID: traits.NewID(), UserID: userID, Kind: domain.GrantWelcome,
			UnitsTotal: units, UnitsRemaining: units, StartsAt: now, Status: domain.GrantActive, GrantedBy: "system"}
		ok, err := insertGrant(ctx, tx, g, now)
		if err != nil || !ok {
			return err
		}
		created = true
		return insertLedger(ctx, tx, domain.LedgerEntry{UserID: userID, GrantID: g.ID, Entry: domain.LedgerGrant,
			Kind: g.Kind, Delta: units, Actor: "system", CreatedAt: now}, "")
	})
	return created, err
}

// InsertCreditGrant — кредит (әкімші, кейін пакет не pass) және журналдағы «grant» бір транзакцияда.
func (s *Store) InsertCreditGrant(ctx context.Context, g domain.CreditGrant, now time.Time) (domain.CreditGrant, error) {
	if g.ID == "" {
		g.ID = traits.NewID()
	}
	if g.Status == "" {
		g.Status = domain.GrantActive
	}
	if g.StartsAt.IsZero() {
		g.StartsAt = now
	}
	if g.GrantedBy == "" {
		g.GrantedBy = "system"
	}
	g.CreatedAt, g.UpdatedAt = now, now
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		ok, err := insertGrant(ctx, tx, g, now)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrConflict
		}
		return insertLedger(ctx, tx, domain.LedgerEntry{UserID: g.UserID, GrantID: g.ID, Entry: domain.LedgerGrant,
			Kind: g.Kind, Delta: g.UnitsTotal, Actor: g.GrantedBy, Reason: g.Reason, CreatedAt: now}, "")
	})
	if err != nil {
		return domain.CreditGrant{}, err
	}
	return g, nil
}

// RevokeCreditGrant — кредитті тоқтатады: қалдығы нөл, журналда «revoke» (теріс емес).
//
// Units already spent stay spent; a generation still running on this grant
// is refunded to nobody. revoked is what was left on the grant.
func (s *Store) RevokeCreditGrant(ctx context.Context, userID, grantID, by, reason string, now time.Time) (domain.CreditGrant, int, error) {
	var (
		g       domain.CreditGrant
		revoked int
	)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT units_remaining, status FROM credit_grants WHERE id = ? AND user_id = ?`,
			grantID, userID).Scan(&revoked, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != domain.GrantActive {
			return domain.ConflictField("grant_id", "revoked")
		}
		at := ms(now)
		if _, err := tx.ExecContext(ctx, `
			UPDATE credit_grants SET status = 'revoked', units_remaining = 0, revoked_at = ?, revoked_by = ?,
			       revoke_reason = ?, updated_at = ?
			WHERE id = ? AND status = 'active'`, at, by, reason, at, grantID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+grantColumns+` FROM credit_grants WHERE id = ?`, grantID)
		if err != nil {
			return err
		}
		list, err := scanGrants(rows)
		if err != nil {
			return err
		}
		if len(list) != 1 {
			return domain.ErrNotFound
		}
		g = list[0]
		return insertLedger(ctx, tx, domain.LedgerEntry{UserID: userID, GrantID: grantID, Entry: domain.LedgerRevoke,
			Kind: g.Kind, Delta: -revoked, Actor: by, Reason: reason, CreatedAt: now}, "")
	})
	if err != nil {
		return domain.CreditGrant{}, 0, err
	}
	return g, revoked, nil
}

const grantColumns = `id, user_id, kind, units_total, units_remaining, starts_at, expires_at, status,
	product_code, granted_by, reason, revoked_at, revoked_by, revoke_reason, created_at, updated_at`

// CreditGrants — қолданушының кредиттері, жаңасы бірінші.
func (s *Store) CreditGrants(ctx context.Context, userID string, limit int) ([]domain.CreditGrant, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT `+grantColumns+` FROM credit_grants WHERE user_id = ? ORDER BY created_at DESC, id LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

func scanGrants(rows *sql.Rows) ([]domain.CreditGrant, error) {
	defer rows.Close()
	var out []domain.CreditGrant
	for rows.Next() {
		var (
			g                        domain.CreditGrant
			starts, created, updated int64
			expires, revoked         sql.NullInt64
		)
		if err := rows.Scan(&g.ID, &g.UserID, &g.Kind, &g.UnitsTotal, &g.UnitsRemaining, &starts, &expires,
			&g.Status, &g.ProductCode, &g.GrantedBy, &g.Reason, &revoked, &g.RevokedBy, &g.RevokeReason,
			&created, &updated); err != nil {
			return nil, err
		}
		g.StartsAt, g.ExpiresAt, g.RevokedAt = timeFrom(starts), timePtr(expires), timePtr(revoked)
		g.CreatedAt, g.UpdatedAt = timeFrom(created), timeFrom(updated)
		out = append(out, g)
	}
	return out, rows.Err()
}

// CreditLedger — қолданушының журналы, жаңасы бірінші.
func (s *Store) CreditLedger(ctx context.Context, userID string, limit int) ([]domain.LedgerEntry, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, user_id, COALESCE(grant_id, ''), entry, kind, delta, actor, reason, created_at
		FROM credit_ledger WHERE user_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LedgerEntry
	for rows.Next() {
		var (
			e       domain.LedgerEntry
			created int64
		)
		if err := rows.Scan(&e.ID, &e.UserID, &e.GrantID, &e.Entry, &e.Kind, &e.Delta, &e.Actor, &e.Reason,
			&created); err != nil {
			return nil, err
		}
		e.CreatedAt = timeFrom(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// insertGrant — бір кредит жолы. false — бірегейлік (бонус бір рет) жолды жазғызбады.
func insertGrant(ctx context.Context, tx *sql.Tx, g domain.CreditGrant, now time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO credit_grants (id, user_id, kind, units_total, units_remaining, starts_at, expires_at, status,
		                           product_code, granted_by, reason, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT DO NOTHING`,
		g.ID, g.UserID, g.Kind, g.UnitsTotal, g.UnitsRemaining, ms(g.StartsAt), msPtr(g.ExpiresAt), g.Status,
		g.ProductCode, g.GrantedBy, g.Reason, ms(now), ms(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// insertLedger — журнал жазбасы; брон бойынша жазба бір рет қана (reservation_id, entry бірегей).
func insertLedger(ctx context.Context, tx *sql.Tx, e domain.LedgerEntry, reservationID string) error {
	if e.Actor == "" {
		e.Actor = "system"
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO credit_ledger (id, user_id, grant_id, reservation_id, entry, kind, delta, actor, reason, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT DO NOTHING`,
		traits.NewID(), e.UserID, nullText(e.GrantID), nullText(reservationID), e.Entry, e.Kind, e.Delta,
		e.Actor, e.Reason, ms(e.CreatedAt))
	return err
}
