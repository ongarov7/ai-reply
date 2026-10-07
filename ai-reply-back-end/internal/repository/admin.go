package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// AdminByEmail — кіру үшін.
func (s *Store) AdminByEmail(ctx context.Context, email string) (domain.AdminUser, error) {
	return s.scanAdmin(s.db.Reader().QueryRowContext(ctx, `
		SELECT id, email, name, password_hash, role, locale, is_active, created_at, updated_at, last_login_at
		FROM admin_users WHERE email = ?`, email))
}

// AdminByID — сессияны тексеру үшін.
func (s *Store) AdminByID(ctx context.Context, id string) (domain.AdminUser, error) {
	return s.scanAdmin(s.db.Reader().QueryRowContext(ctx, `
		SELECT id, email, name, password_hash, role, locale, is_active, created_at, updated_at, last_login_at
		FROM admin_users WHERE id = ?`, id))
}

func (s *Store) scanAdmin(row *sql.Row) (domain.AdminUser, error) {
	var (
		a                domain.AdminUser
		active           int
		created, updated int64
		lastLogin        sql.NullInt64
	)
	err := row.Scan(&a.ID, &a.Email, &a.Name, &a.PasswordHash, &a.Role, &a.Locale, &active,
		&created, &updated, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AdminUser{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AdminUser{}, err
	}
	a.IsActive = active == 1
	a.CreatedAt, a.UpdatedAt, a.LastLoginAt = timeFrom(created), timeFrom(updated), timePtr(lastLogin)
	return a, nil
}

// CreateAdmin — bootstrap немесе қолмен қосу.
func (s *Store) CreateAdmin(ctx context.Context, a domain.AdminUser) (domain.AdminUser, error) {
	if a.ID == "" {
		a.ID = traits.NewID()
	}
	now := time.Now().UTC()
	a.CreatedAt, a.UpdatedAt = now, now
	if a.Role == "" {
		a.Role = "admin"
	}
	if a.Locale == "" {
		a.Locale = "ru"
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO admin_users (id, email, name, password_hash, role, locale, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,?,1,?,?)`,
		a.ID, a.Email, a.Name, a.PasswordHash, a.Role, a.Locale, ms(now), ms(now))
	if isUnique(err) {
		return domain.AdminUser{}, domain.ErrConflict
	}
	a.IsActive = true
	return a, err
}

// UpdateAdminPassword — құпиясөзді ауыстыру (bootstrap кезінде де).
func (s *Store) UpdateAdminPassword(ctx context.Context, id, hash string) error {
	res, err := s.db.Writer().ExecContext(ctx,
		`UPDATE admin_users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, ms(time.Now()), id)
	return affected(res, err)
}

// UpdateAdminLocale — интерфейс тілі есте сақталады.
func (s *Store) UpdateAdminLocale(ctx context.Context, id, locale string) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`UPDATE admin_users SET locale = ?, updated_at = ? WHERE id = ?`, locale, ms(time.Now()), id)
	return err
}

// TouchAdminLogin — соңғы кіру уақыты.
func (s *Store) TouchAdminLogin(ctx context.Context, id string) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`UPDATE admin_users SET last_login_at = ? WHERE id = ?`, ms(time.Now()), id)
	return err
}

// ---------------------------------------------------------------- sessions

// AdminSession — cookie сессиясы (токен хэш түрінде).
type AdminSession struct {
	ID        string
	AdminID   string
	TokenHash string
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	IP        string
	UserAgent string
}

// CreateAdminSession — сәтті кіруден кейін.
func (s *Store) CreateAdminSession(ctx context.Context, sess AdminSession) (AdminSession, error) {
	if sess.ID == "" {
		sess.ID = traits.NewID()
	}
	sess.CreatedAt = time.Now().UTC()
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO admin_sessions (id, admin_id, token_hash, csrf_token, created_at, expires_at, ip, user_agent)
		VALUES (?,?,?,?,?,?,?,?)`,
		sess.ID, sess.AdminID, sess.TokenHash, sess.CSRFToken, ms(sess.CreatedAt), ms(sess.ExpiresAt),
		sess.IP, sess.UserAgent)
	return sess, err
}

// AdminSessionByHash — cookie бойынша.
func (s *Store) AdminSessionByHash(ctx context.Context, hash string) (AdminSession, error) {
	var (
		sess             AdminSession
		created, expires int64
		revoked          sql.NullInt64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, admin_id, token_hash, csrf_token, created_at, expires_at, revoked_at, ip, user_agent
		FROM admin_sessions WHERE token_hash = ?`, hash).
		Scan(&sess.ID, &sess.AdminID, &sess.TokenHash, &sess.CSRFToken, &created, &expires, &revoked,
			&sess.IP, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminSession{}, domain.ErrNotFound
	}
	if err != nil {
		return AdminSession{}, err
	}
	sess.CreatedAt, sess.ExpiresAt, sess.RevokedAt = timeFrom(created), timeFrom(expires), timePtr(revoked)
	return sess, nil
}

// RevokeAdminSession — шығу.
func (s *Store) RevokeAdminSession(ctx context.Context, id string) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`UPDATE admin_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, ms(time.Now()), id)
	return err
}

// ---------------------------------------------------------------- audit

// WriteAudit — әрбір маңызды әкімші әрекеті (құпия мазмұнсыз).
func (s *Store) WriteAudit(ctx context.Context, e domain.AuditEntry) error {
	if e.ID == "" {
		e.ID = traits.NewID()
	}
	meta := "{}"
	if e.Metadata != nil {
		if b, err := json.Marshal(e.Metadata); err == nil {
			meta = string(b)
		}
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO admin_audit_logs (id, admin_id, admin_email, action, entity_type, entity_id, metadata, ip, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		e.ID, e.AdminID, e.AdminEmail, e.Action, e.EntityType, e.EntityID, meta, e.IP, ms(time.Now()))
	return err
}

// AuditLog — соңғы жазбалар.
func (s *Store) AuditLog(ctx context.Context, page traits.Page) ([]domain.AuditEntry, int, error) {
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, admin_id, admin_email, action, entity_type, entity_id, metadata, ip, created_at
		FROM admin_audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.AuditEntry
	for rows.Next() {
		var (
			e       domain.AuditEntry
			meta    string
			created int64
		)
		if err := rows.Scan(&e.ID, &e.AdminID, &e.AdminEmail, &e.Action, &e.EntityType, &e.EntityID,
			&meta, &e.IP, &created); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal([]byte(meta), &e.Metadata)
		e.CreatedAt = timeFrom(created)
		out = append(out, e)
	}
	return out, total, rows.Err()
}
