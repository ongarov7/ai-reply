package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const userColumns = `id, phone, email, status, locale, timezone, platform, app_version, os_version,
	kind, legacy_client, created_at, updated_at, last_active_at`

func scanUser(row interface{ Scan(...any) error }) (domain.User, error) {
	var (
		u                    domain.User
		phone, email, legacy sql.NullString
		created, updated     int64
		lastActive           sql.NullInt64
	)
	err := row.Scan(&u.ID, &phone, &email, &u.Status, &u.Locale, &u.Timezone, &u.Platform,
		&u.AppVersion, &u.OSVersion, &u.Kind, &legacy, &created, &updated, &lastActive)
	if err != nil {
		return domain.User{}, err
	}
	u.Phone, u.Email, u.LegacyClient = text(phone), text(email), text(legacy)
	u.CreatedAt, u.UpdatedAt, u.LastActiveAt = timeFrom(created), timeFrom(updated), timePtr(lastActive)
	return u, nil
}

// CreateUser — жаңа есептік жазба.
func (s *Store) CreateUser(ctx context.Context, u domain.User) (domain.User, error) {
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

	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO users (id, phone, email, status, locale, timezone, platform, app_version, os_version,
		                   kind, legacy_client, created_at, updated_at, last_active_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, nullText(u.Phone), nullText(u.Email), u.Status, u.Locale, u.Timezone, u.Platform,
		u.AppVersion, u.OSVersion, u.Kind, nullText(u.LegacyClient), ms(u.CreatedAt), ms(u.UpdatedAt),
		msPtr(u.LastActiveAt))
	if err != nil {
		if isUnique(err) {
			return domain.User{}, domain.ErrConflict
		}
		return domain.User{}, err
	}
	// Бос профиль бірден жасалады — кейін NULL тексерудің қажеті болмайды.
	_, err = s.db.Writer().ExecContext(ctx,
		`INSERT INTO user_profiles (user_id, updated_at) VALUES (?, ?)`, u.ID, ms(now))
	if err != nil && !isUnique(err) {
		return domain.User{}, err
	}
	return u, nil
}

// UserByID — идентификатор бойынша.
func (s *Store) UserByID(ctx context.Context, id string) (domain.User, error) {
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ? AND deleted_at IS NULL`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// UserByIdentity — телефон не пошта бойынша.
func (s *Store) UserByIdentity(ctx context.Context, kind, value string) (domain.User, error) {
	column := "phone"
	if kind == "email" {
		column = "email"
	}
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE `+column+` = ? AND deleted_at IS NULL`, value)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// UserByLegacyClient — ескі install-token клиенті.
func (s *Store) UserByLegacyClient(ctx context.Context, client string) (domain.User, error) {
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE legacy_client = ? AND deleted_at IS NULL`, client)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// UpdateUserStatus — белсенді/өшірілген.
func (s *Store) UpdateUserStatus(ctx context.Context, id, status string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE users SET status = ?, updated_at = ? WHERE id = ?`, status, ms(time.Now()), id)
	return affected(res, err)
}

// UpdateUserMeta — платформа/нұсқа/тіл сияқты метадерек.
func (s *Store) UpdateUserMeta(ctx context.Context, id string, platform, appVersion, osVersion, locale, tz string) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE users SET
			platform    = CASE WHEN ? <> '' THEN ? ELSE platform END,
			app_version = CASE WHEN ? <> '' THEN ? ELSE app_version END,
			os_version  = CASE WHEN ? <> '' THEN ? ELSE os_version END,
			locale      = CASE WHEN ? <> '' THEN ? ELSE locale END,
			timezone    = CASE WHEN ? <> '' THEN ? ELSE timezone END,
			last_active_at = ?, updated_at = ?
		WHERE id = ?`,
		platform, platform, appVersion, appVersion, osVersion, osVersion,
		locale, locale, tz, tz, ms(time.Now()), ms(time.Now()), id)
	return err
}

// TouchUser — соңғы белсенділік.
func (s *Store) TouchUser(ctx context.Context, id string) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`UPDATE users SET last_active_at = ? WHERE id = ?`, ms(time.Now()), id)
	return err
}

// ---------------------------------------------------------------- profile

// Profile — қолданушы профилі.
func (s *Store) Profile(ctx context.Context, userID string) (domain.Profile, error) {
	var (
		p       domain.Profile
		rules   string
		updated int64
		done    int
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT user_id, display_name, role, description, preferred_tone, business_offering,
		       business_summary, business_rules, onboarding_completed, updated_at
		FROM user_profiles WHERE user_id = ?`, userID).
		Scan(&p.UserID, &p.DisplayName, &p.Role, &p.Description, &p.PreferredTone,
			&p.BusinessOffering, &p.BusinessSummary, &rules, &done, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Profile{UserID: userID, PreferredTone: "natural"}, nil
	}
	if err != nil {
		return domain.Profile{}, err
	}
	p.OnboardingCompleted = done == 1
	p.UpdatedAt = timeFrom(updated)
	_ = json.Unmarshal([]byte(rules), &p.BusinessRules)
	return p, nil
}

// SaveProfile — профильді толық жазады.
func (s *Store) SaveProfile(ctx context.Context, p domain.Profile) error {
	rules, err := json.Marshal(p.BusinessRules)
	if err != nil {
		return err
	}
	done := 0
	if p.OnboardingCompleted {
		done = 1
	}
	_, err = s.db.Writer().ExecContext(ctx, `
		INSERT INTO user_profiles (user_id, display_name, role, description, preferred_tone,
		                           business_offering, business_summary, business_rules,
		                           onboarding_completed, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (user_id) DO UPDATE SET
			display_name = excluded.display_name,
			role = excluded.role,
			description = excluded.description,
			preferred_tone = excluded.preferred_tone,
			business_offering = excluded.business_offering,
			business_summary = excluded.business_summary,
			business_rules = excluded.business_rules,
			onboarding_completed = excluded.onboarding_completed,
			updated_at = excluded.updated_at`,
		p.UserID, p.DisplayName, p.Role, p.Description, p.PreferredTone, p.BusinessOffering,
		p.BusinessSummary, string(rules), done, ms(time.Now()))
	return err
}

// ---------------------------------------------------------------- identities

// SaveIdentity — расталған телефон/пошта.
func (s *Store) SaveIdentity(ctx context.Context, userID, kind, value, country string) error {
	now := time.Now().UTC()
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO auth_identities (id, user_id, kind, value, country, verified_at, created_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (kind, value) DO UPDATE SET verified_at = excluded.verified_at`,
		traits.NewID(), userID, kind, value, country, ms(now), ms(now))
	return err
}

// ---------------------------------------------------------------- listing

// UserFilter — әкімші тізімі үшін сүзгі.
type UserFilter struct {
	Search   string
	UserIDs  []string // nil: no restriction; empty: nothing matched (exact-identity search)
	Status   string
	Platform string
	PlanID   string
	Page     traits.Page
	SortBy   string
	SortDesc bool
}

// UserRow — тізім жолы (қолдану метадерегімен бірге).
type UserRow struct {
	User         domain.User
	PlanCode     string
	PlanName     string
	SubStatus    string
	DailyLimit   int
	UsedToday    int
	TokensMonth  int
	SubExpiresAt *time.Time
}

// ListUsers — сүзгі, іздеу, беттеу.
func (s *Store) ListUsers(ctx context.Context, f UserFilter, today, month string) ([]UserRow, int, error) {
	where := []string{"u.deleted_at IS NULL"}
	args := []any{}
	if f.UserIDs != nil {
		if len(f.UserIDs) == 0 {
			where = append(where, "1 = 0")
		} else {
			where = append(where, "u.id IN ("+placeholders(len(f.UserIDs))+")")
			for _, id := range f.UserIDs {
				args = append(args, id)
			}
		}
	} else if f.Search != "" {
		where = append(where, "(u.phone LIKE ? OR u.email LIKE ? OR u.id LIKE ?)")
		like := "%" + strings.TrimSpace(f.Search) + "%"
		args = append(args, like, like, like)
	}
	if f.Status != "" {
		where = append(where, "u.status = ?")
		args = append(args, f.Status)
	}
	if f.Platform != "" {
		where = append(where, "u.platform = ?")
		args = append(args, f.Platform)
	}
	if f.PlanID != "" {
		where = append(where, "sub.plan_id = ?")
		args = append(args, f.PlanID)
	}

	sortColumn := map[string]string{
		"created_at":  "u.created_at",
		"last_active": "u.last_active_at",
		"used_today":  "used_today",
	}[f.SortBy]
	if sortColumn == "" {
		sortColumn = "u.created_at"
	}
	direction := "ASC"
	if f.SortDesc {
		direction = "DESC"
	}

	base := `
		FROM users u
		LEFT JOIN (
			SELECT s1.* FROM subscriptions s1
			WHERE s1.id = (SELECT s2.id FROM subscriptions s2
			               WHERE s2.user_id = s1.user_id
			               ORDER BY CASE s2.status WHEN 'active' THEN 0 WHEN 'trial' THEN 1 ELSE 2 END,
			                        s2.created_at DESC LIMIT 1)
		) sub ON sub.user_id = u.id
		LEFT JOIN plans p ON p.id = sub.plan_id
		LEFT JOIN usage_daily ud ON ud.user_id = u.id AND ud.usage_date = ?
		LEFT JOIN usage_monthly um ON um.user_id = u.id AND um.usage_month = ?
		WHERE ` + strings.Join(where, " AND ")

	countArgs := append([]any{today, month}, args...)
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) `+base, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + userColumns + `,
		COALESCE(p.code, ''), COALESCE(p.name_en, ''), COALESCE(sub.status, ''),
		COALESCE(p.daily_message_limit, 0), COALESCE(ud.used, 0),
		COALESCE(um.input_tokens, 0) + COALESCE(um.output_tokens, 0), sub.expires_at ` +
		strings.ReplaceAll(base, "FROM users u", "FROM users u") +
		fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", sortColumn, direction)
	// userColumns u. префиксімен жазылуы керек
	query = strings.Replace(query, "SELECT "+userColumns, "SELECT "+prefixColumns(userColumns, "u."), 1)

	rowsArgs := append(append([]any{today, month}, args...), f.Page.Limit, f.Page.Offset)
	rows, err := s.db.Reader().QueryContext(ctx, query, rowsArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []UserRow
	for rows.Next() {
		var (
			r          UserRow
			phone      sql.NullString
			email      sql.NullString
			legacy     sql.NullString
			created    int64
			updated    int64
			lastActive sql.NullInt64
			expires    sql.NullInt64
		)
		if err := rows.Scan(&r.User.ID, &phone, &email, &r.User.Status, &r.User.Locale, &r.User.Timezone,
			&r.User.Platform, &r.User.AppVersion, &r.User.OSVersion, &r.User.Kind, &legacy,
			&created, &updated, &lastActive,
			&r.PlanCode, &r.PlanName, &r.SubStatus, &r.DailyLimit, &r.UsedToday, &r.TokensMonth, &expires); err != nil {
			return nil, 0, err
		}
		r.User.Phone, r.User.Email, r.User.LegacyClient = text(phone), text(email), text(legacy)
		r.User.CreatedAt, r.User.UpdatedAt = timeFrom(created), timeFrom(updated)
		r.User.LastActiveAt = timePtr(lastActive)
		r.SubExpiresAt = timePtr(expires)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func prefixColumns(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
