package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// BindLegacyProDeviceInstallation is called only after authenticated installation
// registration. A historical binding can recover a released client's original
// per-account device after an account switch. It never moves an authorization,
// changes an existing alias, or supplies identity to the strict iid policy.
func (s *Store) BindLegacyProDeviceInstallation(ctx context.Context, userID, deviceID, rawInstallationID string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var platform string
		var revoked sql.NullInt64
		var tombstones int
		err := tx.QueryRowContext(ctx, `SELECT d.platform,d.revoked_at,
 (SELECT COUNT(*) FROM pro_device_authorizations a WHERE a.device_id=d.id AND a.revoked_at IS NOT NULL) +
 (SELECT COUNT(*) FROM pro_device_session_aliases a WHERE a.device_id=d.id AND a.revoked_at IS NOT NULL)
 FROM devices d WHERE d.id=? AND d.user_id=?`, deviceID, userID).Scan(&platform, &revoked, &tombstones)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrProDeviceIdentity
		}
		if err != nil {
			return err
		}
		if revoked.Valid || tombstones != 0 {
			return domain.ErrProDeviceRevoked
		}
		if proCategory(platform) == "" {
			return domain.ErrProDeviceIdentity
		}

		var installationID string
		err = tx.QueryRowContext(ctx, `SELECT id FROM app_installations
 WHERE installation_id=? AND user_id=? AND platform=?`, rawInstallationID, userID, platform).Scan(&installationID)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrProDeviceIdentity
		}
		if err != nil {
			return err
		}

		var canonicalID string
		err = tx.QueryRowContext(ctx, `SELECT device_id FROM pro_device_installations
 WHERE user_id=? AND installation_id=?`, userID, installationID).Scan(&canonicalID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) || canonicalID == deviceID {
			// A source alias cannot later acquire an unrelated installation.
			var aliases int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_session_aliases WHERE device_id=?`, deviceID).Scan(&aliases); err != nil {
				return err
			}
			if aliases != 0 {
				return domain.ErrProDeviceIdentity
			}
			return bindProDeviceInstallation(ctx, tx, userID, deviceID, rawInstallationID)
		}

		var canonicalPlatform string
		err = tx.QueryRowContext(ctx, `SELECT d.platform,d.revoked_at,
 (SELECT COUNT(*) FROM pro_device_authorizations a WHERE a.device_id=d.id AND a.revoked_at IS NOT NULL)
 FROM devices d WHERE d.id=? AND d.user_id=?`, canonicalID, userID).Scan(&canonicalPlatform, &revoked, &tombstones)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && canonicalPlatform != platform) {
			return domain.ErrProDeviceIdentity
		}
		if err != nil {
			return err
		}
		if revoked.Valid || tombstones != 0 {
			return domain.ErrProDeviceRevoked
		}
		var conflicts int
		err = tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM pro_device_authorizations WHERE device_id=?) +
 (SELECT COUNT(*) FROM pro_device_installations WHERE device_id=?) +
 (SELECT COUNT(*) FROM pro_device_session_aliases WHERE device_id=? OR canonical_device_id=?)`,
			deviceID, deviceID, canonicalID, deviceID).Scan(&conflicts)
		if err != nil {
			return err
		}
		if conflicts != 0 {
			return domain.ErrProDeviceIdentity
		}
		var existingCanonical string
		err = tx.QueryRowContext(ctx, `SELECT canonical_device_id,revoked_at FROM pro_device_session_aliases
 WHERE device_id=? AND user_id=?`, deviceID, userID).Scan(&existingCanonical, &revoked)
		if err == nil {
			if revoked.Valid {
				return domain.ErrProDeviceRevoked
			}
			if existingCanonical != canonicalID {
				return domain.ErrProDeviceIdentity
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `INSERT INTO pro_device_session_aliases(device_id,user_id,canonical_device_id,created_at)
 VALUES(?,?,?,?)`, deviceID, userID, canonicalID, ms(now)); err != nil {
			return err
		}
		return insertProDeviceEvent(ctx, tx, domain.ProDeviceEvent{UserID: userID, DeviceID: deviceID,
			Category: proCategory(platform), Platform: platform, Kind: "legacy_session_linked", Mode: "compatibility",
			Reason: "authenticated_installation_registration", CreatedAt: now})
	})
}

// CanonicalProDeviceID resolves only aliases established by authenticated
// registration. Callers must pass the device from a verified session, never an
// AI request body. No alias is inferred from model, IP, account or platform.
func (s *Store) CanonicalProDeviceID(ctx context.Context, userID, deviceID string) (string, error) {
	var platform string
	var revoked, aliasRevoked, canonicalRevoked sql.NullInt64
	var canonicalID, canonicalUser, canonicalPlatform sql.NullString
	var tombstones, canonicalTombstones int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT d.platform,d.revoked_at,
 a.canonical_device_id,a.revoked_at,c.user_id,c.platform,c.revoked_at,
 (SELECT COUNT(*) FROM pro_device_authorizations p WHERE p.device_id=d.id AND p.revoked_at IS NOT NULL),
 (SELECT COUNT(*) FROM pro_device_authorizations p WHERE p.device_id=c.id AND p.revoked_at IS NOT NULL)
 FROM devices d
 LEFT JOIN pro_device_session_aliases a ON a.device_id=d.id AND a.user_id=d.user_id
 LEFT JOIN devices c ON c.id=a.canonical_device_id
 WHERE d.id=? AND d.user_id=?`, deviceID, userID).Scan(&platform, &revoked,
		&canonicalID, &aliasRevoked, &canonicalUser, &canonicalPlatform, &canonicalRevoked, &tombstones, &canonicalTombstones)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrProDeviceIdentity
	}
	if err != nil {
		return "", err
	}
	if revoked.Valid || tombstones != 0 || aliasRevoked.Valid || canonicalRevoked.Valid || canonicalTombstones != 0 {
		return "", domain.ErrProDeviceRevoked
	}
	if proCategory(platform) == "" {
		return "", domain.ErrProDeviceIdentity
	}
	if !canonicalID.Valid {
		return deviceID, nil
	}
	if canonicalUser.String != userID || canonicalPlatform.String != platform {
		return "", domain.ErrProDeviceIdentity
	}
	return canonicalID.String, nil
}
