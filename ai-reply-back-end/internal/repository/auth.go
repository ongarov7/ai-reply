package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// OTPRecord — жіберілген кодтың жазбасы (код тек хэш түрінде).
type OTPRecord struct {
	ID             string
	Kind           string
	Value          string
	Purpose        string
	Channel        string
	CodeHash       string
	Attempts       int
	MaxAttempts    int
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	ConsumedReason string
	CreatedAt      time.Time
}

// CreateOTP — жаңа код жазбасы; ескі белсенділер жабылады.
func (s *Store) CreateOTP(ctx context.Context, rec OTPRecord) (OTPRecord, error) {
	if rec.ID == "" {
		rec.ID = traits.NewID()
	}
	now := time.Now().UTC()
	rec.CreatedAt = now
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE otp_codes SET consumed_at = ?
		WHERE identity_kind = ? AND identity_value = ? AND consumed_at IS NULL`,
		ms(now), rec.Kind, rec.Value)
	if err != nil {
		return OTPRecord{}, err
	}
	_, err = s.db.Writer().ExecContext(ctx, `
		INSERT INTO otp_codes (id, identity_kind, identity_value, channel, code_hash, attempts,
		                       max_attempts, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.Kind, rec.Value, rec.Channel, rec.CodeHash, 0, rec.MaxAttempts,
		ms(rec.ExpiresAt), ms(now))
	return rec, err
}

// ActiveOTP — соңғы жұмсалмаған код.
func (s *Store) ActiveOTP(ctx context.Context, kind, value string) (OTPRecord, error) {
	var (
		rec      OTPRecord
		expires  int64
		created  int64
		consumed sql.NullInt64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, identity_kind, identity_value, channel, code_hash, attempts, max_attempts,
		       expires_at, consumed_at, created_at
		FROM otp_codes
		WHERE identity_kind = ? AND identity_value = ? AND consumed_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, kind, value).
		Scan(&rec.ID, &rec.Kind, &rec.Value, &rec.Channel, &rec.CodeHash, &rec.Attempts,
			&rec.MaxAttempts, &expires, &consumed, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return OTPRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return OTPRecord{}, err
	}
	rec.ExpiresAt, rec.CreatedAt, rec.ConsumedAt = timeFrom(expires), timeFrom(created), timePtr(consumed)
	return rec, nil
}

// IncrementOTPAttempt — әрекет санағышы (brute-force қорғанысы).
func (s *Store) IncrementOTPAttempt(ctx context.Context, id string) (int, error) {
	if _, err := s.db.Writer().ExecContext(ctx,
		`UPDATE otp_codes SET attempts = attempts + 1 WHERE id = ?`, id); err != nil {
		return 0, err
	}
	var attempts int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT attempts FROM otp_codes WHERE id = ?`, id).Scan(&attempts)
	return attempts, err
}

// ConsumeOTP — кодты жабады.
func (s *Store) ConsumeOTP(ctx context.Context, id string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE otp_codes SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, ms(time.Now()), id)
	return affected(res, err)
}

// OTPRequestsSince — сағаттық лимитті тексеру үшін.
func (s *Store) OTPRequestsSince(ctx context.Context, kind, value string, since time.Time) (int, error) {
	var n int
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_codes
		WHERE identity_kind = ? AND identity_value = ? AND created_at >= ?`,
		kind, value, ms(since)).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------- refresh

// RefreshToken — сақталатын refresh жазбасы (ашық мән емес, SHA-256 хэші).
type RefreshToken struct {
	ID        string
	UserID    string
	DeviceID  string
	FamilyID  string
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	Reason    string
	UserAgent string
}

// CreateRefreshToken — жаңа сессия немесе ротация нәтижесі.
func (s *Store) CreateRefreshToken(ctx context.Context, t RefreshToken) (RefreshToken, error) {
	if t.ID == "" {
		t.ID = traits.NewID()
	}
	if t.FamilyID == "" {
		t.FamilyID = t.ID
	}
	t.IssuedAt = time.Now().UTC()
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO refresh_tokens (id, user_id, device_id, family_id, token_hash, issued_at,
		                            expires_at, user_agent)
		VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, nullText(t.DeviceID), t.FamilyID, t.TokenHash, ms(t.IssuedAt),
		ms(t.ExpiresAt), t.UserAgent)
	return t, err
}

// RefreshTokenByHash — берілген токенді табу.
func (s *Store) RefreshTokenByHash(ctx context.Context, hash string) (RefreshToken, error) {
	var (
		t       RefreshToken
		device  sql.NullString
		issued  int64
		expires int64
		revoked sql.NullInt64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, user_id, device_id, family_id, token_hash, issued_at, expires_at, revoked_at,
		       revoked_reason, user_agent
		FROM refresh_tokens WHERE token_hash = ?`, hash).
		Scan(&t.ID, &t.UserID, &device, &t.FamilyID, &t.TokenHash, &issued, &expires, &revoked,
			&t.Reason, &t.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshToken{}, domain.ErrNotFound
	}
	if err != nil {
		return RefreshToken{}, err
	}
	t.DeviceID = text(device)
	t.IssuedAt, t.ExpiresAt, t.RevokedAt = timeFrom(issued), timeFrom(expires), timePtr(revoked)
	return t, nil
}

// RotateRefreshToken — ескіні жабады, жаңасын байланыстырады (бір транзакцияда).
func (s *Store) RotateRefreshToken(ctx context.Context, oldID string, next RefreshToken) (RefreshToken, error) {
	if next.ID == "" {
		next.ID = traits.NewID()
	}
	next.IssuedAt = time.Now().UTC()
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE refresh_tokens SET revoked_at = ?, revoked_reason = 'rotated', replaced_by = ?
			WHERE id = ? AND revoked_at IS NULL`, ms(next.IssuedAt), next.ID, oldID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrUnauthorized
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO refresh_tokens (id, user_id, device_id, family_id, token_hash, issued_at,
			                            expires_at, user_agent)
			VALUES (?,?,?,?,?,?,?,?)`,
			next.ID, next.UserID, nullText(next.DeviceID), next.FamilyID, next.TokenHash,
			ms(next.IssuedAt), ms(next.ExpiresAt), next.UserAgent)
		return err
	})
	return next, err
}

// RevokeRefreshToken — шығу.
func (s *Store) RevokeRefreshToken(ctx context.Context, id, reason string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ?, revoked_reason = ? WHERE id = ? AND revoked_at IS NULL`,
		ms(time.Now()), reason, id)
	return affected(res, err)
}

// RevokeFamily — қайта пайдалану анықталғанда бүкіл тізбекті жабады.
func (s *Store) RevokeFamily(ctx context.Context, familyID, reason string) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ?, revoked_reason = ? WHERE family_id = ? AND revoked_at IS NULL`,
		ms(time.Now()), reason, familyID)
	return err
}

// RevokeUserSessions — әкімшінің «барлық сессияны жабу» әрекеті.
func (s *Store) RevokeUserSessions(ctx context.Context, userID, reason string) (int64, error) {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ?, revoked_reason = ? WHERE user_id = ? AND revoked_at IS NULL`,
		ms(time.Now()), reason, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ActiveSessions — қолданушының ашық сессиялары.
func (s *Store) ActiveSessions(ctx context.Context, userID string) ([]RefreshToken, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, user_id, device_id, family_id, token_hash, issued_at, expires_at, revoked_at,
		       revoked_reason, user_agent
		FROM refresh_tokens WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		ORDER BY issued_at DESC`, userID, ms(time.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefreshToken
	for rows.Next() {
		var (
			t       RefreshToken
			device  sql.NullString
			issued  int64
			expires int64
			revoked sql.NullInt64
		)
		if err := rows.Scan(&t.ID, &t.UserID, &device, &t.FamilyID, &t.TokenHash, &issued, &expires,
			&revoked, &t.Reason, &t.UserAgent); err != nil {
			return nil, err
		}
		t.DeviceID = text(device)
		t.IssuedAt, t.ExpiresAt, t.RevokedAt = timeFrom(issued), timeFrom(expires), timePtr(revoked)
		out = append(out, t)
	}
	return out, rows.Err()
}
