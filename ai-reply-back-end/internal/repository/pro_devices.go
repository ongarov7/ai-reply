package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const proAuthorizationColumns = `a.id,a.user_id,a.device_id,a.category,a.platform,a.display_name,
 a.first_authorized_at,MAX(a.last_seen_at,COALESCE(d.last_seen_at,0)),a.revoked_at,a.revoked_reason`

func scanProAuthorization(row interface{ Scan(...any) error }) (domain.ProDeviceAuthorization, error) {
	var a domain.ProDeviceAuthorization
	var first, last int64
	var revoked sql.NullInt64
	err := row.Scan(&a.ID, &a.UserID, &a.DeviceID, &a.Category, &a.Platform, &a.DisplayName,
		&first, &last, &revoked, &a.RevokedReason)
	if errors.Is(err, sql.ErrNoRows) {
		return a, domain.ErrNotFound
	}
	a.FirstAuthorizedAt, a.LastSeenAt, a.RevokedAt = timeFrom(first), timeFrom(last), timePtr(revoked)
	return a, err
}

// ProDeviceAuthorization includes tombstones: a later login must not undo an
// explicit revocation even though legacy UpsertDevice clears devices.revoked_at.
func (s *Store) ProDeviceAuthorization(ctx context.Context, userID, deviceID string) (domain.ProDeviceAuthorization, error) {
	return scanProAuthorization(s.db.Reader().QueryRowContext(ctx, `SELECT `+proAuthorizationColumns+`
 FROM pro_device_authorizations a JOIN devices d ON d.id=a.device_id
 WHERE a.user_id=? AND a.device_id=?`, userID, deviceID))
}

func proCategory(platform string) string {
	switch platform {
	case "ios", "android", "ipados":
		return domain.ProDeviceMobile
	case "macos", "windows":
		return domain.ProDeviceDesktop
	default:
		return ""
	}
}

// ClaimProDevice serializes with other device changes through BEGIN IMMEDIATE.
// No notification delivery or other network operation occurs in this transaction.
func (s *Store) ClaimProDevice(ctx context.Context, userID, deviceID, category, platform, displayName string, now time.Time) (domain.ProDeviceAuthorization, bool, error) {
	var out domain.ProDeviceAuthorization
	created := false
	if platform != strings.ToLower(strings.TrimSpace(platform)) || proCategory(platform) != category || category == "" {
		return out, false, domain.ErrProDeviceIdentity
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var storedPlatform string
		var revoked sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT platform,revoked_at FROM devices WHERE id=? AND user_id=?`,
			deviceID, userID).Scan(&storedPlatform, &revoked)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (revoked.Valid || strings.ToLower(strings.TrimSpace(storedPlatform)) != platform)) {
			return domain.ErrProDeviceIdentity
		}
		if err != nil {
			return err
		}
		out, err = scanProAuthorization(tx.QueryRowContext(ctx, `SELECT `+proAuthorizationColumns+`
 FROM pro_device_authorizations a JOIN devices d ON d.id=a.device_id WHERE a.user_id=? AND a.device_id=?`, userID, deviceID))
		if err == nil {
			if out.RevokedAt != nil {
				return domain.ErrProDeviceRevoked
			}
			if out.Category != category || out.Platform != platform {
				return domain.ErrProDeviceIdentity
			}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		out = domain.ProDeviceAuthorization{ID: traits.NewID(), UserID: userID, DeviceID: deviceID,
			Category: category, Platform: platform, DisplayName: proMetadata(displayName, 80),
			FirstAuthorizedAt: now, LastSeenAt: now}
		res, err := tx.ExecContext(ctx, `INSERT INTO pro_device_authorizations
 (id,user_id,device_id,category,platform,display_name,first_authorized_at,last_seen_at)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, out.ID, userID, deviceID, category, platform,
			out.DisplayName, ms(now), ms(now))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrProDeviceLimit
		}
		created = true
		return insertProDeviceEvent(ctx, tx, domain.ProDeviceEvent{UserID: userID, DeviceID: deviceID,
			Category: category, Platform: platform, Kind: "authorized", Mode: "enforce", CreatedAt: now})
	})
	if err != nil {
		return domain.ProDeviceAuthorization{}, false, err
	}
	return out, created, nil
}

// proMetadata keeps display/audit text bounded and single-line. Identifiers and
// tokens must never be supplied in these descriptive fields by callers.
func proMetadata(v string, limit int) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, v)
	return traits.Clamp(v, limit)
}

func insertProDeviceEvent(ctx context.Context, tx *sql.Tx, e domain.ProDeviceEvent) error {
	if e.ID == "" {
		e.ID = traits.NewID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO pro_device_events
 (id,user_id,device_id,category,platform,kind,mode,reason,created_at,notify_requested)
 VALUES(?,?,(SELECT id FROM devices WHERE id=? AND user_id=?),?,?,?,?,?,?,?)`, e.ID, e.UserID,
		e.DeviceID, e.UserID, proMetadata(e.Category, 16), proMetadata(e.Platform, 16), proMetadata(e.Kind, 48),
		proMetadata(e.Mode, 16), proMetadata(e.Reason, 200), ms(e.CreatedAt), e.NotificationRequested && e.Kind == "limit_reached" && e.Mode == "enforce")
	return err
}

func (s *Store) RecordProDeviceEvent(ctx context.Context, e domain.ProDeviceEvent) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error { return insertProDeviceEvent(ctx, tx, e) })
}

func (s *Store) ProDeviceAuthorizations(ctx context.Context, userID string) ([]domain.ProDeviceAuthorization, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+proAuthorizationColumns+`
 FROM pro_device_authorizations a JOIN devices d ON d.id=a.device_id WHERE a.user_id=? ORDER BY a.first_authorized_at,a.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProDeviceAuthorization{}
	for rows.Next() {
		a, err := scanProAuthorization(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ProDeviceEvents(ctx context.Context, userID string, limit int) ([]domain.ProDeviceEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id,user_id,COALESCE(device_id,''),category,platform,kind,mode,reason,created_at
 FROM pro_device_events WHERE user_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProDeviceEvent{}
	for rows.Next() {
		var e domain.ProDeviceEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.UserID, &e.DeviceID, &e.Category, &e.Platform, &e.Kind, &e.Mode, &e.Reason, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = timeFrom(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// RevokeProDevice releases exactly one slot and revokes every refresh family
// for that installation atomically. Non-admin callers must themselves occupy a
// slot (or be the device being removed), so a blocked login cannot evict its owner.
// Revoked identities remain tombstones; reactivation requires explicit support.
func (s *Store) RevokeProDevice(ctx context.Context, userID, authorizationID, actorID, reason string, admin bool, now time.Time) error {
	reason = proMetadata(reason, 200)
	if reason == "" {
		return domain.InvalidField("reason", "required")
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		a, err := scanProAuthorization(tx.QueryRowContext(ctx, `SELECT `+proAuthorizationColumns+`
 FROM pro_device_authorizations a JOIN devices d ON d.id=a.device_id WHERE a.id=? AND a.user_id=?`, authorizationID, userID))
		if err != nil {
			return err
		}
		if admin {
			var allowed int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users WHERE id=? AND is_active=1 AND role='admin'`, actorID).Scan(&allowed); err != nil {
				return err
			}
			if allowed != 1 {
				return domain.ErrProDeviceManagementForbidden
			}
		} else if actorID != a.DeviceID || a.RevokedAt == nil {
			var allowed int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_authorizations p
 JOIN devices d ON d.id=p.device_id AND d.user_id=p.user_id
 WHERE p.user_id=? AND p.device_id=? AND p.revoked_at IS NULL AND d.revoked_at IS NULL`, userID, actorID).Scan(&allowed); err != nil {
				return err
			}
			if allowed != 1 {
				return domain.ErrProDeviceManagementForbidden
			}
		}
		if a.RevokedAt != nil {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pro_device_authorizations SET revoked_at=?,revoked_reason=? WHERE id=? AND revoked_at IS NULL`, ms(now), reason, authorizationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET revoked_at=?,push_token=NULL,push_enabled=0 WHERE id=? AND user_id=?`, ms(now), a.DeviceID, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=?,revoked_reason='pro_device_revoked'
 WHERE user_id=? AND device_id=? AND revoked_at IS NULL`, ms(now), userID, a.DeviceID); err != nil {
			return err
		}
		// A released device must no longer receive future security pushes.
		if _, err := tx.ExecContext(ctx, `UPDATE app_installations SET user_id=NULL,attached_at=NULL,updated_at=?
 WHERE id=(SELECT installation_id FROM pro_device_installations WHERE device_id=?) AND user_id=?`, ms(now), a.DeviceID, userID); err != nil {
			return err
		}
		if err := insertProDeviceEvent(ctx, tx, domain.ProDeviceEvent{UserID: userID, DeviceID: a.DeviceID,
			Category: a.Category, Platform: a.Platform, Kind: "revoked", Mode: "enforce", Reason: reason, CreatedAt: now}); err != nil {
			return err
		}
		if admin {
			meta, err := json.Marshal(map[string]string{"user_id": userID, "device_category": a.Category, "reason": reason})
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO admin_audit_logs(id,admin_id,action,entity_type,entity_id,metadata,created_at)
 VALUES(?,?,'pro_device.revoked','pro_device_authorization',?,?,?)`, traits.NewID(), actorID, a.ID, string(meta), ms(now))
			return err
		}
		return nil
	})
}

// BindProDeviceInstallation links explicit existing registrations. A conflicting
// link is evidence of cloned/restored identifiers and must be resolved manually.
func (s *Store) BindProDeviceInstallation(ctx context.Context, userID, deviceID, installationID string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var internalID string
		err := tx.QueryRowContext(ctx, `SELECT i.id FROM app_installations i JOIN devices d ON d.id=?
 WHERE i.installation_id=? AND i.user_id=? AND d.user_id=? AND d.revoked_at IS NULL
 AND lower(trim(d.platform))=i.platform`, deviceID, installationID, userID, userID).Scan(&internalID)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrProDeviceIdentity
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pro_device_installations(device_id,user_id,installation_id) VALUES(?,?,?)
 ON CONFLICT DO NOTHING`, deviceID, userID, internalID); err != nil {
			return err
		}
		var linked string
		err = tx.QueryRowContext(ctx, `SELECT installation_id FROM pro_device_installations WHERE device_id=?`, deviceID).Scan(&linked)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && linked != internalID) {
			return domain.ErrProDeviceIdentity
		}
		return err
	})
}

func (s *Store) ProDevicePushTargets(ctx context.Context, userID, deviceID string) ([]string, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT i.id FROM pro_device_authorizations a
 JOIN devices d ON d.id=a.device_id AND d.user_id=a.user_id
 JOIN pro_device_installations p ON p.device_id=a.device_id AND p.user_id=a.user_id
 JOIN app_installations i ON i.id=p.installation_id AND i.user_id=a.user_id
 WHERE a.user_id=? AND a.device_id<>? AND a.revoked_at IS NULL AND d.revoked_at IS NULL
 AND lower(trim(d.platform))=a.platform AND i.platform=a.platform ORDER BY a.category`, userID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ProDeviceIdentityReady requires an explicit link and current ownership in
// both registries. It makes no claim of hardware attestation or clone detection.
func (s *Store) ProDeviceIdentityReady(ctx context.Context, userID, deviceID string) (bool, error) {
	var n int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_installations p
 JOIN devices d ON d.id=p.device_id AND d.user_id=p.user_id
 JOIN app_installations i ON i.id=p.installation_id AND i.user_id=p.user_id
 WHERE p.user_id=? AND p.device_id=? AND d.revoked_at IS NULL AND lower(trim(d.platform))=i.platform`, userID, deviceID).Scan(&n)
	return n == 1, err
}

// ProDeviceRawInstallationID is internal identity material for comparing a
// signed session binding. Callers must never return or log its result.
func (s *Store) ProDeviceRawInstallationID(ctx context.Context, userID, deviceID string) (string, error) {
	var raw string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT i.installation_id FROM pro_device_installations p
 JOIN devices d ON d.id=p.device_id AND d.user_id=p.user_id
 JOIN app_installations i ON i.id=p.installation_id AND i.user_id=p.user_id
 WHERE p.user_id=? AND p.device_id=? AND d.revoked_at IS NULL AND lower(trim(d.platform))=i.platform`, userID, deviceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return raw, err
}

// DeviceByProInstallation is intended only after successful login verification.
// Ownership of a raw installation ID alone never authenticates an account.
// The historical per-user association remains usable after an account switch.
func (s *Store) DeviceByProInstallation(ctx context.Context, userID, rawInstallationID string) (domain.Device, error) {
	var id string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT d.id FROM pro_device_installations p
 JOIN devices d ON d.id=p.device_id AND d.user_id=p.user_id
 JOIN app_installations i ON i.id=p.installation_id
 WHERE p.user_id=? AND i.installation_id=?`, userID, rawInstallationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Device{}, err
	}
	return s.Device(ctx, id)
}

// ObserveProDevice records distinct installations without assigning a winner.
// Repeated requests avoid write contention inside the 15-minute activity window;
// attempt_count counts observation windows, not every individual API request.
func (s *Store) ObserveProDevice(ctx context.Context, userID, deviceID, category, platform string, now time.Time) (bool, error) {
	if proCategory(platform) != category || category == "" {
		return false, domain.ErrProDeviceIdentity
	}
	var last int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT last_seen_at FROM pro_device_observations
 WHERE user_id=? AND device_id=?`, userID, deviceID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if errors.Is(err, sql.ErrNoRows) || last < ms(now.Add(-15*time.Minute)) {
		err = s.db.Tx(ctx, func(tx *sql.Tx) error {
			var valid int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id=? AND user_id=?
 AND revoked_at IS NULL AND lower(trim(platform))=?`, deviceID, userID, platform).Scan(&valid); err != nil {
				return err
			}
			if valid != 1 {
				return domain.ErrProDeviceIdentity
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO pro_device_observations
 (user_id,device_id,category,platform,first_seen_at,last_seen_at,attempt_count) VALUES(?,?,?,?,?,?,1)
 ON CONFLICT(user_id,device_id) DO UPDATE SET last_seen_at=excluded.last_seen_at,attempt_count=attempt_count+1
 WHERE pro_device_observations.last_seen_at < ? AND pro_device_observations.category=excluded.category
 AND pro_device_observations.platform=excluded.platform`, userID, deviceID, category, platform, ms(now), ms(now), ms(now.Add(-15*time.Minute)))
			return err
		})
		if err != nil {
			return false, err
		}
	}
	var count int
	err = s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_observations WHERE user_id=? AND category=?`, userID, category).Scan(&count)
	return count > 1, err
}

// TouchProDevice updates activity at most once per 15-minute window. The read
// fast path avoids taking the SQLite writer lock on every successful request.
func (s *Store) TouchProDevice(ctx context.Context, userID, deviceID string, now time.Time) error {
	var last int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT last_seen_at FROM pro_device_authorizations
 WHERE user_id=? AND device_id=? AND revoked_at IS NULL`, userID, deviceID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil || last >= ms(now.Add(-15*time.Minute)) {
		return err
	}
	_, err = s.db.Writer().ExecContext(ctx, `UPDATE pro_device_authorizations SET last_seen_at=?
 WHERE user_id=? AND device_id=? AND revoked_at IS NULL AND last_seen_at<?`, ms(now), userID, deviceID, ms(now.Add(-15*time.Minute)))
	return err
}
