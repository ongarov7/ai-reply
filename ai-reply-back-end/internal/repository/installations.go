package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const installationColumns = `id, installation_id, user_id, platform, app_version, app_build, os_name, os_version,
	device_model, manufacturer, locale, timezone, push_provider, push_token_hash,
	push_permission, notifications_enabled, push_status, push_status_reason, token_updated_at,
	push_disabled_at, attached_at, first_seen_at, last_seen_at, created_at, updated_at`

// scanInstallation — installationColumns, then any extra columns into extra.
func scanInstallation(row interface{ Scan(...any) error }, extra ...any) (domain.Installation, error) {
	var (
		i                                   domain.Installation
		userID, tokenHash                   sql.NullString
		enabled                             int
		tokenUpdated, disabled, attached    sql.NullInt64
		firstSeen, lastSeen, created, updtd int64
	)
	dest := append([]any{&i.ID, &i.InstallationID, &userID, &i.Platform, &i.AppVersion, &i.AppBuild,
		&i.OSName, &i.OSVersion, &i.DeviceModel, &i.Manufacturer, &i.Locale, &i.Timezone,
		&i.PushProvider, &tokenHash, &i.PushPermission, &enabled,
		&i.PushStatus, &i.PushStatusReason, &tokenUpdated, &disabled, &attached,
		&firstSeen, &lastSeen, &created, &updtd}, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.Installation{}, err
	}
	i.UserID, i.PushTokenHash = text(userID), text(tokenHash)
	i.NotificationsEnabled = enabled == 1
	i.TokenUpdatedAt, i.PushDisabledAt, i.AttachedAt = timePtr(tokenUpdated), timePtr(disabled), timePtr(attached)
	i.FirstSeenAt, i.LastSeenAt = timeFrom(firstSeen), timeFrom(lastSeen)
	i.CreatedAt, i.UpdatedAt = timeFrom(created), timeFrom(updtd)
	return i, nil
}

// InstallationUpsert — тіркеу не жаңарту сұранысы (сервис тексеріп, токенді шифрлаған).
type InstallationUpsert struct {
	InstallationID       string
	UserID               string // "" — анонимді: орнату тіркелгіден ажыратылады
	Platform             string
	AppVersion           string
	AppBuild             string
	OSName               string
	OSVersion            string
	DeviceModel          string
	Manufacturer         string
	Locale               string
	Timezone             string
	Permission           string
	NotificationsEnabled bool
	// Push token: only when the app sent one.
	HasToken    bool
	Provider    string
	TokenSealed string
	TokenHash   string
	Now         time.Time
}

// InstallationChange — тіркеу нәтижесінде не өзгерді (журнал үшін).
type InstallationChange struct {
	Created        bool
	PreviousUserID string
	TokenChanged   bool
	TokenMoved     int64 // how many other installations lost this token
}

// UpsertInstallation — бір транзакцияда: токенді басқа орнатудан алу, жолды жазу.
//
// The token uniqueness index guarantees one installation per push token; the
// device that presents a token now is the one that owns it, so any other row
// holding it (an old install of the same phone) loses it first. The user
// association always comes from the caller's authentication, never from the
// request body: UserID "" detaches the installation. A token the provider
// reported dead stays invalid when the app registers it again: the app is
// told to fetch a new one instead of having pushes queued that fail again.
func (s *Store) UpsertInstallation(ctx context.Context, in InstallationUpsert) (domain.Installation, InstallationChange, error) {
	var change InstallationChange
	now := ms(in.Now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var (
			id           string
			prevUser     sql.NullString
			prevHash     sql.NullString
			prevStatus   string
			prevAttached sql.NullInt64
		)
		err := tx.QueryRowContext(ctx, `
			SELECT id, user_id, push_token_hash, push_status, attached_at FROM app_installations WHERE installation_id = ?`,
			in.InstallationID).Scan(&id, &prevUser, &prevHash, &prevStatus, &prevAttached)
		exists := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		change.PreviousUserID = text(prevUser)

		if in.HasToken {
			res, err := tx.ExecContext(ctx, `
				UPDATE app_installations
				SET push_token_sealed = NULL, push_token_hash = NULL, push_status = 'replaced',
				    push_status_reason = 'token_moved', push_disabled_at = ?, updated_at = ?
				WHERE push_provider = ? AND push_token_hash = ? AND installation_id <> ?`,
				now, now, in.Provider, in.TokenHash, in.InstallationID)
			if err != nil {
				return err
			}
			change.TokenMoved, _ = res.RowsAffected()
			change.TokenChanged = !exists || text(prevHash) != in.TokenHash
		}

		var attachedAt any
		switch {
		case in.UserID == "":
			attachedAt = nil
		case in.UserID == change.PreviousUserID && prevAttached.Valid:
			attachedAt = prevAttached.Int64
		default:
			attachedAt = now
		}
		enabled := 0
		if in.NotificationsEnabled {
			enabled = 1
		}

		if !exists {
			id = traits.NewID()
			change.Created = true
			var sealed, hash, tokenUpdated any
			provider, status := domain.ProviderFCM, domain.PushNone
			if in.HasToken {
				sealed, hash, provider, tokenUpdated, status = in.TokenSealed, in.TokenHash, in.Provider, now, domain.PushActive
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO app_installations (id, installation_id, user_id, platform, app_version, app_version_num,
				    app_build, os_name, os_version, os_version_num, device_model, manufacturer, locale, timezone,
				    push_provider, push_token_sealed, push_token_hash, push_permission,
				    notifications_enabled, push_status, token_updated_at, attached_at, first_seen_at,
				    last_seen_at, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				id, in.InstallationID, nullText(in.UserID), in.Platform, in.AppVersion, domain.VersionNumber(in.AppVersion),
				in.AppBuild, in.OSName, in.OSVersion, domain.VersionNumber(in.OSVersion), in.DeviceModel,
				in.Manufacturer, in.Locale, in.Timezone, provider, sealed, hash, in.Permission,
				enabled, status, tokenUpdated, attachedAt, now, now, now, now)
			return err
		}

		// The provider reported this very token dead (its hash was kept for that).
		deadToken := prevStatus == domain.PushInvalid && text(prevHash) == in.TokenHash
		if in.HasToken && !deadToken {
			_, err = tx.ExecContext(ctx, `
				UPDATE app_installations SET
					push_provider = ?, push_token_sealed = ?, push_token_hash = ?,
					token_updated_at = CASE WHEN push_token_hash IS ? THEN token_updated_at ELSE ? END,
					push_status = 'active', push_status_reason = '', push_disabled_at = NULL
				WHERE id = ?`,
				in.Provider, in.TokenSealed, in.TokenHash, in.TokenHash, now, id)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE app_installations SET
				user_id = ?, attached_at = ?, platform = ?,
				app_version = ?, app_version_num = ?, app_build = ?, os_name = ?, os_version = ?,
				os_version_num = ?, device_model = ?, manufacturer = ?, locale = ?, timezone = ?,
				push_permission = ?, notifications_enabled = ?, last_seen_at = ?, updated_at = ?
			WHERE id = ?`,
			nullText(in.UserID), attachedAt, in.Platform,
			in.AppVersion, domain.VersionNumber(in.AppVersion), in.AppBuild, in.OSName, in.OSVersion,
			domain.VersionNumber(in.OSVersion), in.DeviceModel, in.Manufacturer, in.Locale, in.Timezone,
			in.Permission, enabled, now, now, id)
		return err
	})
	if err != nil {
		if isUnique(err) {
			return domain.Installation{}, change, domain.ErrConflict
		}
		return domain.Installation{}, change, err
	}
	inst, err := s.InstallationByClientID(ctx, in.InstallationID)
	return inst, change, err
}

// InstallationByClientID — қосымша жасаған идентификатор бойынша.
func (s *Store) InstallationByClientID(ctx context.Context, installationID string) (domain.Installation, error) {
	inst, err := scanInstallation(s.db.Reader().QueryRowContext(ctx,
		`SELECT `+installationColumns+` FROM app_installations WHERE installation_id = ?`, installationID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Installation{}, domain.ErrNotFound
	}
	return inst, err
}

// DetachInstallation — орнатуды қолданушыдан ажыратады (тек сол қолданушыныкі болса).
func (s *Store) DetachInstallation(ctx context.Context, installationID, userID string, now time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE app_installations SET user_id = NULL, attached_at = NULL, updated_at = ?
		WHERE installation_id = ? AND user_id = ?`, ms(now), installationID, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DetachInstallationAny — орнатуды кімге тіркелгеніне қарамай ажыратады.
func (s *Store) DetachInstallationAny(ctx context.Context, installationID string, now time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE app_installations SET user_id = NULL, attached_at = NULL, updated_at = ?
		WHERE installation_id = ? AND user_id IS NOT NULL`, ms(now), installationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DetachUserInstallations — қолданушының барлық орнатуын ажыратады (сессиялар жабылғанда).
func (s *Store) DetachUserInstallations(ctx context.Context, userID string, now time.Time) (int64, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE app_installations SET user_id = NULL, attached_at = NULL, updated_at = ?
		WHERE user_id = ?`, ms(now), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// InvalidateInstallationToken — провайдер «өлі» деген токенді өшіреді.
//
// Only if the installation still holds that very token: the app may have
// registered a fresh one while the failed message was in flight. With final
// the token itself is dead: its hash stays on the row, so registering that
// same token again keeps the installation invalid (UpsertInstallation).
func (s *Store) InvalidateInstallationToken(ctx context.Context, id, tokenHash, reason string, final bool, now time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE app_installations SET
			push_token_sealed = NULL, push_token_hash = CASE WHEN ? = 1 THEN push_token_hash END,
			push_status = 'invalid', push_status_reason = ?, push_disabled_at = ?, updated_at = ?
		WHERE id = ? AND push_token_hash = ?`, boolInt(final), reason, ms(now), ms(now), id, tokenHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// InstallationFilter — әкімші панеліндегі құрылғылар тізімі.
type InstallationFilter struct {
	Search     string // installation id prefix, user id, device model
	UserID     string
	Platform   string
	PushStatus string
	AppVersion string
	Auth       string // "authenticated" | "anonymous" | ""
	Page       traits.Page
}

// Орнатудың тіркелгіге байланысы (құрылғылар тізімінің сүзгісі).
const (
	InstallationsAttached  = "authenticated"
	InstallationsAnonymous = "anonymous"
)

// shortInstallationID — how many characters of an installation id admins see.
const shortInstallationID = 8

// escapeLike — the value matched literally inside LIKE … ESCAPE '\'.
func escapeLike(v string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(v)
}

// InstallationRow — тізім жолы (қолданушының идентификаторымен бірге).
type InstallationRow struct {
	Installation domain.Installation
	UserEmail    string
	UserPhone    string
}

// ListInstallations — сүзгі және беттеу. Токеннің өзі ешқашан оқылмайды.
func (s *Store) ListInstallations(ctx context.Context, f InstallationFilter) ([]InstallationRow, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := escapeLike(q)
		conditions := []string{`i.user_id LIKE ? ESCAPE '\'`, `i.device_model LIKE ? ESCAPE '\'`, `i.id LIKE ? ESCAPE '\'`}
		args = append(args, like+"%", "%"+like+"%", like+"%")
		// The app-generated id is matched only by the short prefix the panel
		// shows: a longer prefix would let someone recover the whole id one
		// character at a time (and the whole id can detach the phone).
		if len(q) <= shortInstallationID {
			conditions = append(conditions, `i.installation_id LIKE ? ESCAPE '\'`)
			args = append(args, like+"%")
		}
		where = append(where, "("+strings.Join(conditions, " OR ")+")")
	}
	for column, value := range map[string]string{
		"i.user_id": f.UserID, "i.platform": f.Platform, "i.push_status": f.PushStatus, "i.app_version": f.AppVersion,
	} {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	switch f.Auth {
	case InstallationsAttached:
		where = append(where, "i.user_id IS NOT NULL")
	case InstallationsAnonymous:
		where = append(where, "i.user_id IS NULL")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM app_installations i WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT `+prefixColumns(installationColumns, "i.")+`, COALESCE(u.email, ''), COALESCE(u.phone, '')
		FROM app_installations i LEFT JOIN users u ON u.id = i.user_id
		WHERE `+clause+` ORDER BY i.last_seen_at DESC, i.id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []InstallationRow
	for rows.Next() {
		var row InstallationRow
		inst, err := scanInstallation(rows, &row.UserEmail, &row.UserPhone)
		if err != nil {
			return nil, 0, err
		}
		row.Installation = inst
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// SendTarget — жіберу сәтіндегі орнату күйі (шифрланған токенмен, тек жұмысшыға).
type SendTarget struct {
	InstallationID       string
	UserID               string
	Platform             string
	Provider             string
	TokenSealed          string
	TokenHash            string
	PushStatus           string
	Permission           string
	NotificationsEnabled bool
}

// InstallationSendTarget — жұмысшы жіберер алдында оқиды.
func (s *Store) InstallationSendTarget(ctx context.Context, id string) (SendTarget, error) {
	var (
		t              SendTarget
		userID, sealed sql.NullString
		hash           sql.NullString
		enabled        int
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, user_id, platform, push_provider, push_token_sealed, push_token_hash,
		       push_status, push_permission, notifications_enabled
		FROM app_installations WHERE id = ?`, id).
		Scan(&t.InstallationID, &userID, &t.Platform, &t.Provider, &sealed, &hash,
			&t.PushStatus, &t.Permission, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return SendTarget{}, domain.ErrNotFound
	}
	t.UserID, t.TokenSealed, t.TokenHash = text(userID), text(sealed), text(hash)
	t.NotificationsEnabled = enabled == 1
	return t, err
}
