package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// UpsertDevice — құрылғыны тіркейді немесе жаңартады.
func (s *Store) UpsertDevice(ctx context.Context, d domain.Device) (domain.Device, error) {
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = traits.NewID()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.LastSeenAt = now
	push := 0
	if d.PushOn {
		push = 1
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO devices (id, user_id, platform, app_version, os_version, model, locale,
		                     push_token, push_enabled, created_at, last_seen_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET
			platform = excluded.platform,
			app_version = excluded.app_version,
			os_version = excluded.os_version,
			model = excluded.model,
			locale = excluded.locale,
			push_token = COALESCE(excluded.push_token, devices.push_token),
			push_enabled = excluded.push_enabled,
			last_seen_at = excluded.last_seen_at,
			revoked_at = NULL`,
		d.ID, d.UserID, d.Platform, d.AppVersion, d.OSVersion, d.Model, d.Locale,
		nullText(d.PushToken), push, ms(d.CreatedAt), ms(d.LastSeenAt))
	return d, err
}

// Device — бір құрылғы.
func (s *Store) Device(ctx context.Context, id string) (domain.Device, error) {
	row := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, user_id, platform, app_version, os_version, model, locale, push_token,
		       push_enabled, created_at, last_seen_at, revoked_at
		FROM devices WHERE id = ?`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, domain.ErrNotFound
	}
	return d, err
}

// DevicesByUser — қолданушының құрылғылары.
func (s *Store) DevicesByUser(ctx context.Context, userID string) ([]domain.Device, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, user_id, platform, app_version, os_version, model, locale, push_token,
		       push_enabled, created_at, last_seen_at, revoked_at
		FROM devices WHERE user_id = ? AND revoked_at IS NULL ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeDevice — құрылғыны өшіру (push та тоқтайды).
func (s *Store) RevokeDevice(ctx context.Context, userID, id string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE devices SET revoked_at = ?, push_token = NULL, push_enabled = 0
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, ms(time.Now()), id, userID)
	return affected(res, err)
}

func scanDevice(row interface{ Scan(...any) error }) (domain.Device, error) {
	var (
		d        domain.Device
		push     sql.NullString
		enabled  int
		created  int64
		lastSeen int64
		revoked  sql.NullInt64
	)
	if err := row.Scan(&d.ID, &d.UserID, &d.Platform, &d.AppVersion, &d.OSVersion, &d.Model,
		&d.Locale, &push, &enabled, &created, &lastSeen, &revoked); err != nil {
		return domain.Device{}, err
	}
	d.PushToken, d.PushOn = text(push), enabled == 1
	d.CreatedAt, d.LastSeenAt, d.RevokedAt = timeFrom(created), timeFrom(lastSeen), timePtr(revoked)
	return d, nil
}
