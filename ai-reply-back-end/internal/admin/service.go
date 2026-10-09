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
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Events — әкімші әрекетінен кейінгі оқиғалар (хабарлама қабаты іске асырады).
// The call never fails the admin action: the implementation logs its own errors.
type Events interface {
	PlanAssigned(ctx context.Context, sub domain.Subscription)
}

// Service — әкімші әрекеттері.
type Service struct {
	repo   *repository.Store
	subs   *subscriptions.Service
	plans  *plans.Service
	cfg    config.Config
	log    *slog.Logger
	clock  traits.Clock
	events Events
}

// New — қызмет.
func New(repo *repository.Store, subs *subscriptions.Service, planSvc *plans.Service, cfg config.Config, log *slog.Logger) *Service {
	return &Service{repo: repo, subs: subs, plans: planSvc, cfg: cfg, log: log, clock: traits.SystemClock{}}
}

// WithEvents — тариф тағайындау оқиғаларын тыңдаушы (nil — жоқ).
func (s *Service) WithEvents(e Events) *Service { s.events = e; return s }

// Bootstrap — .env-тегі әкімшіні бір рет жасайды (құпиясөз бірден хэштеледі).
//
// When the admin already exists and ADMIN_PASSWORD no longer matches, the new
// value is a rotation: it is hashed in, every open session of that admin is
// closed and the change is audited. Changing the variable and restarting is
// how a leaked password is replaced.
func (s *Service) Bootstrap(ctx context.Context) error {
	email := strings.ToLower(strings.TrimSpace(s.cfg.Admin.BootstrapEmail))
	if email == "" || s.cfg.Admin.BootstrapPassword == "" {
		return nil
	}
	if existing, err := s.repo.AdminByEmail(ctx, email); err == nil {
		return s.rotatePassword(ctx, existing)
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

// rotatePassword — ADMIN_PASSWORD өзгерген болса, хэшті жаңартып, сессияларды жабады.
func (s *Service) rotatePassword(ctx context.Context, admin domain.AdminUser) error {
	if auth.VerifyPassword(admin.PasswordHash, s.cfg.Admin.BootstrapPassword) == nil {
		return nil
	}
	hash, err := auth.HashPassword(s.cfg.Admin.BootstrapPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateAdminPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	revoked, err := s.repo.RevokeAdminSessions(ctx, admin.ID)
	if err != nil {
		return err
	}
	s.Audit(ctx, admin, "", "admin.password_rotated", "admin_user", admin.ID, map[string]any{"sessions_revoked": revoked})
	s.log.Info("admin password rotated", "admin_id", admin.ID, "sessions_revoked", revoked)
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
//
// An unknown address costs the same PBKDF2 run as a wrong password, so the
// response time does not tell which addresses are admins. Every failure is
// audited with the IP; the password never is, and an address that is not an
// admin is not recorded either (people type passwords into the e-mail field).
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	admin, err := s.repo.AdminByEmail(ctx, email)
	if err != nil {
		auth.VerifyDummyPassword(password)
		if !errors.Is(err, domain.ErrNotFound) {
			return Session{}, err
		}
		s.Audit(ctx, domain.AdminUser{}, ip, "admin.login_failed", "admin_user", "", map[string]any{"reason": "unknown_email"})
		return Session{}, domain.ErrUnauthorized
	}
	if err := auth.VerifyPassword(admin.PasswordHash, password); err != nil {
		s.Audit(ctx, admin, ip, "admin.login_failed", "admin_user", admin.ID, map[string]any{"reason": "wrong_password"})
		return Session{}, domain.ErrUnauthorized
	}
	if !admin.IsActive {
		s.Audit(ctx, admin, ip, "admin.login_failed", "admin_user", admin.ID, map[string]any{"reason": "disabled"})
		return Session{}, domain.ErrAccountDisabled
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
		EntityType: entityType, EntityID: entityID, Metadata: meta, IP: ip,
	}); err != nil {
		s.log.Error("audit write failed", "error", err.Error())
	}
}

// Now — барлық қабат үшін ортақ уақыт көзі (тестте жалған сағат).
func (s *Service) Now() time.Time { return s.subs.Now() }

// AuditLog — журнал.
func (s *Service) AuditLog(ctx context.Context, page traits.Page) ([]domain.AuditEntry, int, error) {
	return s.repo.AuditLog(ctx, page)
}

// Notifications — push арналарының күйі.
func (s *Service) Config() config.Config { return s.cfg }
