package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// execer — *sql.DB мен *sql.Tx-тің ортақ бөлігі.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// UserByAuthIdentity — кіру тәсілі (kind, value) арқылы қолданушы.
func (s *Store) UserByAuthIdentity(ctx context.Context, kind, value string) (domain.User, error) {
	row := s.db.Reader().QueryRowContext(ctx, `
		SELECT `+prefixColumns(userColumns, "u.")+`
		FROM auth_identities ai
		JOIN users u ON u.id = ai.user_id
		WHERE ai.kind = ? AND ai.value = ? AND u.deleted_at IS NULL`, kind, value)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// CreateUserWithIdentities — қолданушы, бос профиль және кіру тәсілдері бір транзакцияда.
//
// All rows exist afterwards or none do. A request racing for the same
// provider account or the same e-mail gets ErrConflict, and the caller reads
// back the account the winner created instead of making a second one.
func (s *Store) CreateUserWithIdentities(ctx context.Context, u domain.User, emailVerifiedAt *time.Time,
	identities ...domain.Identity) (domain.User, error) {
	if u.ID == "" {
		u.ID = traits.NewID()
	}
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	if u.Status == "" {
		u.Status = domain.UserActive
	}
	if u.Kind == "" {
		u.Kind = "account"
	}
	u.Locale = domain.NormalizeLocale(u.Locale)

	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, phone, email, email_verified_at, status, locale, timezone, platform,
			                   app_version, os_version, kind, legacy_client, created_at, updated_at, last_active_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			u.ID, nullText(u.Phone), nullText(u.Email), msPtr(emailVerifiedAt), u.Status, u.Locale, u.Timezone,
			u.Platform, u.AppVersion, u.OSVersion, u.Kind, nullText(u.LegacyClient), ms(u.CreatedAt),
			ms(u.UpdatedAt), msPtr(u.LastActiveAt)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_profiles (user_id, updated_at) VALUES (?, ?)`, u.ID, ms(now)); err != nil {
			return err
		}
		for _, identity := range identities {
			if err := insertIdentity(ctx, tx, u.ID, identity, now); err != nil {
				return err
			}
		}
		return nil
	})
	if isUnique(err) {
		return domain.User{}, domain.ErrConflict
	}
	if err != nil {
		return domain.User{}, err
	}
	return u, nil
}

// LinkIdentity — бар қолданушыға жаңа кіру тәсілін қосады. Тәсіл бос болмаса — ErrConflict.
func (s *Store) LinkIdentity(ctx context.Context, userID string, identity domain.Identity) error {
	err := insertIdentity(ctx, s.db.Writer(), userID, identity, time.Now().UTC())
	if isUnique(err) {
		return domain.ErrConflict
	}
	return err
}

// RefreshIdentity — провайдер берген поштаны жаңартады: ол уақыт өте өзгеруі мүмкін.
// Бос пошта бұрынғысын өшірмейді (Apple кейде оны жібермейді).
func (s *Store) RefreshIdentity(ctx context.Context, kind, value, providerEmail string, verified bool) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE auth_identities SET
			provider_email          = CASE WHEN ? <> '' THEN ? ELSE provider_email END,
			provider_email_verified = CASE WHEN ? <> '' THEN ? ELSE provider_email_verified END,
			updated_at = ?
		WHERE kind = ? AND value = ?`,
		providerEmail, providerEmail, providerEmail, boolInt(verified), ms(time.Now()), kind, value)
	return err
}

// ConfirmEmail — OTP-мен дәлелденген поштаны тіркелгіге бекітеді.
//
// users.email_verified_at gets its first verification time and the address
// becomes an explicit e-mail sign-in identity of the same account.
func (s *Store) ConfirmEmail(ctx context.Context, userID, email string, at time.Time) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET email_verified_at = COALESCE(email_verified_at, ?), updated_at = ?
			WHERE id = ? AND email = ?`, ms(at), ms(at), userID, email); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO auth_identities (id, user_id, kind, value, country, provider_email,
			                             provider_email_verified, verified_at, created_at, updated_at)
			VALUES (?,?,?,?,'',?,1,?,?,?)
			ON CONFLICT (kind, value) DO UPDATE SET
				verified_at = excluded.verified_at, updated_at = excluded.updated_at
			WHERE auth_identities.user_id = excluded.user_id`,
			traits.NewID(), userID, domain.IdentityEmail, email, email, ms(at), ms(at), ms(at))
		return err
	})
}

// AttachEmail — поштасы жоқ тіркелгіге расталған поштаны қосады.
//
// ErrConflict: the account already has an address. ErrEmailInUse: another
// account holds this one (the unique constraints decide, not a racy lookup).
func (s *Store) AttachEmail(ctx context.Context, userID, email string, at time.Time) error {
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE users SET email = ?, email_verified_at = ?, updated_at = ?
			WHERE id = ? AND email IS NULL AND deleted_at IS NULL`, email, ms(at), ms(at), userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrConflict
		}
		verified := at
		return insertIdentity(ctx, tx, userID, domain.Identity{
			Kind: domain.IdentityEmail, Value: email, ProviderEmail: email,
			ProviderEmailVerified: true, VerifiedAt: &verified,
		}, at)
	})
	if isUnique(err) {
		return domain.ErrEmailInUse
	}
	return err
}

// SignInMethods — қолданушының кіру тәсілдері, тұрақты ретпен (apple, email, google, phone).
func (s *Store) SignInMethods(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT DISTINCT kind FROM auth_identities WHERE user_id = ? ORDER BY kind`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	methods := []string{}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		methods = append(methods, kind)
	}
	return methods, rows.Err()
}

// SetDisplayNameIfEmpty — провайдер берген атты тек бос профильге жазады.
func (s *Store) SetDisplayNameIfEmpty(ctx context.Context, userID, name string) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE user_profiles SET display_name = ?, updated_at = ?
		WHERE user_id = ? AND display_name = ''`, name, ms(time.Now()), userID)
	return err
}

func insertIdentity(ctx context.Context, db execer, userID string, identity domain.Identity, now time.Time) error {
	if identity.ID == "" {
		identity.ID = traits.NewID()
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO auth_identities (id, user_id, kind, value, country, provider_email,
		                             provider_email_verified, verified_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		identity.ID, userID, identity.Kind, identity.Value, identity.Country, identity.ProviderEmail,
		boolInt(identity.ProviderEmailVerified), msPtr(identity.VerifiedAt), ms(now), ms(now))
	return err
}
