// Package admin — әкімші панелінің логикасы: кіру, аудит, аналитика.
package admin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/redact"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Service — әкімші әрекеттері.
type Service struct {
	repo   *repository.Store
	subs   *subscriptions.Service
	plans  *plans.Service
	cfg    config.Config
	log    *slog.Logger
	clock  traits.Clock
	hasher SubjectHasher
}

// New — қызмет.
func New(repo *repository.Store, subs *subscriptions.Service, planSvc *plans.Service, cfg config.Config, log *slog.Logger) *Service {
	return &Service{repo: repo, subs: subs, plans: planSvc, cfg: cfg, log: log, clock: traits.SystemClock{}}
}

// Bootstrap — .env-тегі әкімшіні бір рет жасайды (құпиясөз бірден хэштеледі).
func (s *Service) Bootstrap(ctx context.Context) error {
	email := strings.ToLower(strings.TrimSpace(s.cfg.Admin.BootstrapEmail))
	if email == "" || s.cfg.Admin.BootstrapPassword == "" {
		return nil
	}
	if _, err := s.repo.AdminByEmail(ctx, email); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	hash, err := auth.HashPassword(s.cfg.Admin.BootstrapPassword)
	if err != nil {
		return err
	}
	admin, err := s.repo.CreateAdmin(ctx, domain.AdminUser{
		Email: email, Name: "Administrator", PasswordHash: hash, Role: "admin", Locale: "ru",
	})
	if err != nil {
		return err
	}
	s.log.Info("admin bootstrapped", "admin_id", admin.ID)
	return nil
}

// Session — ашылған сессия.
type Session struct {
	Token     string
	CSRF      string
	Admin     domain.AdminUser
	ExpiresAt time.Time
}

// Login — email + құпиясөз. Қате себебі клиентке ажыратылмайды.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	admin, err := s.repo.AdminByEmail(ctx, email)
	if err != nil {
		return Session{}, domain.ErrUnauthorized
	}
	if !admin.IsActive {
		return Session{}, domain.ErrAccountDisabled
	}
	if err := auth.VerifyPassword(admin.PasswordHash, password); err != nil {
		return Session{}, domain.ErrUnauthorized
	}

	token := traits.RandomToken(32)
	sess, err := s.repo.CreateAdminSession(ctx, repository.AdminSession{
		AdminID:   admin.ID,
		TokenHash: auth.HashToken(s.cfg.Auth.AccessSecret, token),
		CSRFToken: traits.RandomToken(16),
		ExpiresAt: s.clock.Now().Add(s.cfg.Admin.SessionTTL),
		IP:        ip,
		UserAgent: traits.Clamp(userAgent, 200),
	})
	if err != nil {
		return Session{}, err
	}
	_ = s.repo.TouchAdminLogin(ctx, admin.ID)
	s.Audit(ctx, admin, ip, "admin.login", "admin_user", admin.ID, nil)

	return Session{Token: token, CSRF: sess.CSRFToken, Admin: admin, ExpiresAt: sess.ExpiresAt}, nil
}

// Authenticate — cookie токенін тексеру.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.AdminUser, repository.AdminSession, error) {
	if strings.TrimSpace(token) == "" {
		return domain.AdminUser{}, repository.AdminSession{}, domain.ErrUnauthorized
	}
	sess, err := s.repo.AdminSessionByHash(ctx, auth.HashToken(s.cfg.Auth.AccessSecret, token))
	if err != nil {
		return domain.AdminUser{}, repository.AdminSession{}, domain.ErrUnauthorized
	}
	now := s.clock.Now()
	if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
		return domain.AdminUser{}, repository.AdminSession{}, domain.ErrUnauthorized
	}
	admin, err := s.repo.AdminByID(ctx, sess.AdminID)
	if err != nil || !admin.IsActive {
		return domain.AdminUser{}, repository.AdminSession{}, domain.ErrUnauthorized
	}
	return admin, sess, nil
}

// Logout — сессияны жабу.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	return s.repo.RevokeAdminSession(ctx, sessionID)
}

// SetLocale — интерфейс тілін сақтау.
func (s *Service) SetLocale(ctx context.Context, adminID, locale string) error {
	return s.repo.UpdateAdminLocale(ctx, adminID, domain.NormalizeLocale(locale))
}

// Audit — маңызды әрекетті жазу (мазмұнсыз метадерек).
func (s *Service) Audit(ctx context.Context, admin domain.AdminUser, ip, action, entityType, entityID string, meta map[string]any) {
	if err := s.repo.WriteAudit(ctx, domain.AuditEntry{
		AdminID: admin.ID, AdminEmail: admin.Email, Action: action,
		EntityType: entityType, EntityID: entityID, Metadata: redact.Map(meta), IP: ip,
		RequestID: logging.RequestID(ctx),
	}); err != nil {
		s.log.Error("audit write failed", "error", err.Error())
	}
}

// AuditLogFiltered — сүзгімен аудит журналы.
func (s *Service) AuditLogFiltered(ctx context.Context, f repository.AuditFilter) ([]domain.AuditEntry, int, error) {
	return s.repo.AuditLogFiltered(ctx, f)
}

// Now — барлық қабат үшін ортақ уақыт көзі (тестте жалған сағат).
func (s *Service) Now() time.Time { return s.subs.Now() }

// AuditLog — журнал.
func (s *Service) AuditLog(ctx context.Context, page traits.Page) ([]domain.AuditEntry, int, error) {
	return s.repo.AuditLog(ctx, page)
}

// Notifications — push арналарының күйі.
func (s *Service) Config() config.Config { return s.cfg }
