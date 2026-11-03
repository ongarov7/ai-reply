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
//
// The id comes from the client, so it may name a row that belongs to another
// account (a phone handed over, or a forged id). That row is never touched:
// the device is stored under a fresh server id instead, which the caller
// gets back and puts into the session. A push token another row already
// holds is not copied onto the new row.
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
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO devices (id, user_id, platform, app_version, os_version, model, locale,
			                     push_token, push_enabled, created_at, last_seen_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT (id) DO UPDATE SET
				platform = CASE WHEN
                    EXISTS (SELECT 1 FROM pro_device_authorizations WHERE device_id = devices.id)
                    OR EXISTS (SELECT 1 FROM pro_device_installations WHERE device_id = devices.id)
                    THEN devices.platform ELSE excluded.platform END,
				app_version = excluded.app_version,
				os_version = excluded.os_version,
				model = excluded.model,
				locale = excluded.locale,
				push_token = COALESCE(excluded.push_token, devices.push_token),
				push_enabled = excluded.push_enabled,
				last_seen_at = excluded.last_seen_at,
				revoked_at = NULL
			WHERE devices.user_id = excluded.user_id`,
			d.ID, d.UserID, d.Platform, d.AppVersion, d.OSVersion, d.Model, d.Locale,
			nullText(d.PushToken), push, ms(d.CreatedAt), ms(d.LastSeenAt))
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n > 0 {
			return err
		}
		// The id is another account's device.
		d.ID, d.CreatedAt = traits.NewID(), now
		_, err = tx.ExecContext(ctx, `
			INSERT INTO devices (id, user_id, platform, app_version, os_version, model, locale,
			                     push_token, push_enabled, created_at, last_seen_at)
			VALUES (?,?,?,?,?,?,?,
			        CASE WHEN EXISTS (SELECT 1 FROM devices WHERE push_token = ?) THEN NULL ELSE ? END,
			        ?,?,?)`,
			d.ID, d.UserID, d.Platform, d.AppVersion, d.OSVersion, d.Model, d.Locale,
			nullText(d.PushToken), nullText(d.PushToken), push, ms(d.CreatedAt), ms(d.LastSeenAt))
		return err
	})
	if err != nil {
		return domain.Device{}, err
	}
	// Protected identities retain their canonical platform. Return the stored
	// row so a later token cannot accidentally adopt the caller's rejected
	// platform (or a fabricated creation timestamp).
	return s.Device(ctx, d.ID)
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
	// The legacy management path must not race a first Pro authorization.
	// SQLite serializes this conditional write with ClaimProDevice's writer
	// transaction: either this revoke wins and Claim rejects the device, or
	// the claim wins and only the trusted Pro release path may revoke it.
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE devices SET revoked_at = ?, push_token = NULL, push_enabled = 0
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL
		 AND NOT EXISTS (SELECT 1 FROM pro_device_authorizations WHERE device_id = devices.id)`, ms(time.Now()), id, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	var protected int
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pro_device_authorizations WHERE device_id=? AND user_id=?`, id, userID).Scan(&protected); err != nil {
		return err
	}
	if protected != 0 {
		return domain.ErrProDeviceManagementForbidden
	}
	return domain.ErrNotFound
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
