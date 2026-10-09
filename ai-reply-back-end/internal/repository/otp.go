package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Поштаға келетін кодтардың жазбалары. Код тек HMAC түрінде, ашық мәні жоқ.

const otpColumns = `id, identity_kind, identity_value, purpose, channel, code_hash, attempts,
	max_attempts, expires_at, consumed_at, consumed_reason, created_at`

func scanOTP(row interface{ Scan(...any) error }) (OTPRecord, error) {
	var (
		rec      OTPRecord
		expires  int64
		created  int64
		consumed sql.NullInt64
	)
	err := row.Scan(&rec.ID, &rec.Kind, &rec.Value, &rec.Purpose, &rec.Channel, &rec.CodeHash,
		&rec.Attempts, &rec.MaxAttempts, &expires, &consumed, &rec.ConsumedReason, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return OTPRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return OTPRecord{}, err
	}
	rec.ExpiresAt, rec.CreatedAt, rec.ConsumedAt = timeFrom(expires), timeFrom(created), timePtr(consumed)
	return rec, nil
}

// LatestOTP — идентификаторға соңғы шығарылған код (жабылғаны да қайтады).
func (s *Store) LatestOTP(ctx context.Context, kind, value string) (OTPRecord, error) {
	return scanOTP(s.db.Reader().QueryRowContext(ctx, `
		SELECT `+otpColumns+` FROM otp_codes
		WHERE identity_kind = ? AND identity_value = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, kind, value))
}

// OTPByID — бір жазба.
func (s *Store) OTPByID(ctx context.Context, id string) (OTPRecord, error) {
	return scanOTP(s.db.Reader().QueryRowContext(ctx,
		`SELECT `+otpColumns+` FROM otp_codes WHERE id = ?`, id))
}

// OTPIssuedSince — берілген сәттен бері шығарылған кодтардың уақыты, өсу ретімен.
//
// A code whose delivery failed never reached anyone: it does not count
// toward the per-address caps, so a mail outage cannot lock an address out.
func (s *Store) OTPIssuedSince(ctx context.Context, kind, value string, since time.Time) ([]time.Time, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT created_at FROM otp_codes
		WHERE identity_kind = ? AND identity_value = ? AND created_at >= ? AND consumed_reason <> ?
		ORDER BY created_at`, kind, value, ms(since), domain.OTPReasonDeliveryFailed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var created int64
		if err := rows.Scan(&created); err != nil {
			return nil, err
		}
		out = append(out, timeFrom(created))
	}
	return out, rows.Err()
}

// OTPFailure — бір кодтың қате енгізулері.
type OTPFailure struct {
	CreatedAt time.Time
	Failed    int
}

// OTPFailuresSince — берілген сәттен бері шығарылған кодтардың қате енгізулері, өсу ретімен.
//
// Every guess is counted in attempts; on a verified code the last attempt
// was the right one. Codes without a wrong guess are left out.
func (s *Store) OTPFailuresSince(ctx context.Context, kind, value string, since time.Time) ([]OTPFailure, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT created_at, CASE WHEN consumed_reason = ? THEN attempts - 1 ELSE attempts END AS failed
		FROM otp_codes
		WHERE identity_kind = ? AND identity_value = ? AND created_at >= ? AND attempts > 0
		ORDER BY created_at`, domain.OTPReasonVerified, kind, value, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OTPFailure
	for rows.Next() {
		var (
			f       OTPFailure
			created int64
		)
		if err := rows.Scan(&created, &f.Failed); err != nil {
			return nil, err
		}
		if f.Failed > 0 {
			f.CreatedAt = timeFrom(created)
			out = append(out, f)
		}
	}
	return out, rows.Err()
}

// IssueOTP — жаңа кодты жазады, ал сол идентификатордың ашық кодтарын жабады.
//
// Runs as one write transaction (BEGIN IMMEDIATE on the single writer), so
// two requests for the same mailbox cannot both pass the resend check: a code
// created after quietSince — other than one whose delivery failed — makes the
// call fail with ErrOTPCooldown and nothing is written.
func (s *Store) IssueOTP(ctx context.Context, rec OTPRecord, now, quietSince time.Time) (OTPRecord, error) {
	if rec.ID == "" {
		rec.ID = traits.NewID()
	}
	rec.CreatedAt = now
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var recent int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM otp_codes
			WHERE identity_kind = ? AND identity_value = ? AND created_at > ? AND consumed_reason <> ?`,
			rec.Kind, rec.Value, ms(quietSince), domain.OTPReasonDeliveryFailed).Scan(&recent); err != nil {
			return err
		}
		if recent > 0 {
			return domain.ErrOTPCooldown
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE otp_codes SET consumed_at = ?, consumed_reason = ?, updated_at = ?
			WHERE identity_kind = ? AND identity_value = ? AND consumed_at IS NULL`,
			ms(now), domain.OTPReasonSuperseded, ms(now), rec.Kind, rec.Value); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO otp_codes (id, identity_kind, identity_value, purpose, channel, code_hash, attempts,
			                       max_attempts, expires_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,0,?,?,?,?)`,
			rec.ID, rec.Kind, rec.Value, rec.Purpose, rec.Channel, rec.CodeHash, rec.MaxAttempts,
			ms(rec.ExpiresAt), ms(now), ms(now))
		return err
	})
	if err != nil {
		return OTPRecord{}, err
	}
	return rec, nil
}

// RegisterOTPAttempt — әрекетті атомарлы түрде санап, жаңа санды қайтарады.
//
// The increment and the limit check are one statement, so parallel guesses
// cannot squeeze past max_attempts. A closed code, or one that has used up
// its attempts, returns ErrNotFound.
func (s *Store) RegisterOTPAttempt(ctx context.Context, id string, now time.Time) (int, error) {
	var attempts int
	err := s.db.Writer().QueryRowContext(ctx, `
		UPDATE otp_codes SET attempts = attempts + 1, updated_at = ?
		WHERE id = ? AND consumed_at IS NULL AND attempts < max_attempts
		RETURNING attempts`, ms(now), id).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	return attempts, err
}

// CloseOTP — кодты себебімен жабады. Бұрын жабылған болса — ErrNotFound:
// бір кодпен тек бір сұраныс кіре алады.
func (s *Store) CloseOTP(ctx context.Context, id, reason string, now time.Time) error {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE otp_codes SET consumed_at = ?, consumed_reason = ?, updated_at = ?
		WHERE id = ? AND consumed_at IS NULL`, ms(now), reason, ms(now), id)
	return affected(res, err)
}
