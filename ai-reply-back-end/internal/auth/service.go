package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Provisioner — жаңа қолданушыға бастапқы тарифті береді (subscriptions қызметі).
type Provisioner interface {
	EnsureSubscription(ctx context.Context, userID string) error
}

// Service — кіру сценарийлері.
type Service struct {
	repo   *repository.Store
	cfg    config.Auth
	send   Sender
	prov   Provisioner
	log    *slog.Logger
	clock  traits.Clock
	random io.Reader
	// otpKey — поштаға жіберілген кодтардың HMAC кілті (JWT құпиясынан туындатылған).
	otpKey []byte
	// mail — хат жеткізушісі (Resend); nil болса, тек демо режимде жұмыс істейді.
	mail email.Sender
	// google, apple — токен тексерушілері; nil болса, провайдер өшірулі.
	google IDTokenVerifier
	apple  IDTokenVerifier
}

// New — қызметті құрады.
func New(repo *repository.Store, cfg config.Auth, sender Sender, prov Provisioner, log *slog.Logger) *Service {
	return &Service{
		repo: repo, cfg: cfg, send: sender, prov: prov, log: log, clock: traits.SystemClock{},
		random: rand.Reader, otpKey: deriveKey(cfg.AccessSecret, "ai-reply/email-otp/v1"),
	}
}

// WithClock — тестте уақытты басқару үшін.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// WithRandom — тестте код генераторын бекіту үшін.
func (s *Service) WithRandom(r io.Reader) *Service { s.random = r; return s }

// WithEmailSender — поштаға код жіберетін провайдер.
func (s *Service) WithEmailSender(sender email.Sender) *Service { s.mail = sender; return s }

// WithIdentityProviders — Google және Apple тексерушілері; nil — провайдер өшірулі.
func (s *Service) WithIdentityProviders(google, apple IDTokenVerifier) *Service {
	s.google, s.apple = google, apple
	return s
}

// EmailDelivery — поштаға код қалай жетеді: "email" (провайдер), "demo" немесе "off".
func (s *Service) EmailDelivery() string {
	switch {
	case s.mail != nil:
		return "email"
	case s.cfg.DemoMode:
		return "demo"
	default:
		return "off"
	}
}

// GoogleEnabled — Google арқылы кіру бапталған ба.
func (s *Service) GoogleEnabled() bool { return s.google != nil }

// AppleEnabled — Apple арқылы кіру бапталған ба.
func (s *Service) AppleEnabled() bool { return s.apple != nil }

// Challenge — OTP сұранысының нәтижесі.
type Challenge struct {
	Kind      string
	Masked    string
	Channel   string
	ExpiresIn int
	DemoMode  bool
}

// DeviceInfo — кіру кезінде клиент беретін метадерек.
type DeviceInfo struct {
	DeviceID   string
	Platform   string
	AppVersion string
	OSVersion  string
	Model      string
	Locale     string
	Timezone   string
	UserAgent  string
}

// Session — шығарылған токендер.
type Session struct {
	User             domain.User
	AccessToken      string
	RefreshToken     string
	AccessExpiresIn  int
	RefreshExpiresAt time.Time
	DeviceID         string
	IsNewUser        bool
}

// RequestOTP — кодты сұрау. Демо режимінде код тұрақты (AUTH_DEMO_OTP).
func (s *Service) RequestOTP(ctx context.Context, rawIdentifier, locale string) (Challenge, error) {
	identity, err := ParseIdentity(rawIdentifier)
	if err != nil {
		return Challenge{}, domain.ErrInvalidRequest
	}
	if identity.Kind == domain.IdentityEmail {
		// Older builds reach e-mail through this endpoint: same flow, same limits.
		challenge, err := s.RequestEmailOTP(ctx, identity.Value, locale)
		if err != nil {
			return Challenge{}, err
		}
		channel := "email"
		if challenge.DemoMode {
			channel = "stub"
		}
		return Challenge{Kind: identity.Kind, Masked: challenge.MaskedEmail, Channel: channel,
			ExpiresIn: challenge.ExpiresIn, DemoMode: challenge.DemoMode}, nil
	}

	// Бір идентификаторға сағатына шектеу (brute-force және шығын қорғанысы).
	since := s.clock.Now().Add(-time.Hour)
	count, err := s.repo.OTPRequestsSince(ctx, identity.Kind, identity.Value, since)
	if err != nil {
		return Challenge{}, err
	}
	if count >= s.cfg.OTPRequestsPerHour {
		return Challenge{}, domain.ErrRateLimited
	}

	code := s.cfg.DemoOTP
	if !s.cfg.DemoMode {
		if code, err = NewOTPCode(s.random); err != nil {
			return Challenge{}, err
		}
	}

	if !s.cfg.DemoMode {
		// Нақты жеткізу болмаса — «жіберілді» деп алдамаймыз.
		dst := Destination{Kind: identity.Kind, Value: identity.Value, Country: identity.Country, Locale: locale}
		if err := s.send.Send(ctx, dst, code); err != nil {
			return Challenge{}, err
		}
	}

	expires := s.clock.Now().Add(s.cfg.OTPTTL)
	if _, err := s.repo.CreateOTP(ctx, repository.OTPRecord{
		Kind:        identity.Kind,
		Value:       identity.Value,
		Channel:     s.send.Channel(),
		CodeHash:    HashCode(s.cfg.AccessSecret, identity.Kind, identity.Value, code),
		MaxAttempts: s.cfg.OTPMaxAttempts,
		ExpiresAt:   expires,
	}); err != nil {
		return Challenge{}, err
	}

	s.log.Info("otp requested", "kind", identity.Kind, "country", identity.Country,
		"channel", s.send.Channel(), "demo", s.cfg.DemoMode)

	return Challenge{
		Kind:      identity.Kind,
		Masked:    identity.Masked,
		Channel:   s.send.Channel(),
		ExpiresIn: int(s.cfg.OTPTTL.Seconds()),
		DemoMode:  s.cfg.DemoMode,
	}, nil
}

// VerifyOTP — кодты тексеріп, сессия ашады. Жаңа қолданушы осы жерде жасалады.
func (s *Service) VerifyOTP(ctx context.Context, rawIdentifier, code string, info DeviceInfo) (Session, error) {
	identity, err := ParseIdentity(rawIdentifier)
	if err != nil {
		return Session{}, domain.ErrInvalidRequest
	}
	if identity.Kind == domain.IdentityEmail {
		return s.VerifyEmailOTP(ctx, identity.Value, code, info)
	}
	if strings.TrimSpace(code) == "" {
		return Session{}, domain.ErrInvalidOTP
	}

	record, err := s.repo.ActiveOTP(ctx, identity.Kind, identity.Value)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, domain.ErrInvalidOTP
	}
	if err != nil {
		return Session{}, err
	}
	now := s.clock.Now()
	if now.After(record.ExpiresAt) {
		_ = s.repo.ConsumeOTP(ctx, record.ID)
		return Session{}, domain.ErrOTPExpired
	}
	if record.Attempts >= record.MaxAttempts {
		_ = s.repo.ConsumeOTP(ctx, record.ID)
		return Session{}, domain.ErrRateLimited
	}
	if !CompareCode(record.CodeHash, HashCode(s.cfg.AccessSecret, identity.Kind, identity.Value, code)) {
		attempts, _ := s.repo.IncrementOTPAttempt(ctx, record.ID)
		if attempts >= record.MaxAttempts {
			_ = s.repo.ConsumeOTP(ctx, record.ID)
		}
		return Session{}, domain.ErrInvalidOTP
	}
	if err := s.repo.ConsumeOTP(ctx, record.ID); err != nil {
		return Session{}, err
	}

	user, isNew, err := s.findOrCreatePhoneUser(ctx, identity, info)
	if err != nil {
		return Session{}, err
	}
	return s.openSession(ctx, user, isNew, info)
}

// findOrCreatePhoneUser — телефонмен кірген қолданушы (ескі build-тер үшін сақталған ағын).
func (s *Service) findOrCreatePhoneUser(ctx context.Context, identity Identity, info DeviceInfo) (domain.User, bool, error) {
	user, err := s.repo.UserByIdentity(ctx, identity.Kind, identity.Value)
	switch {
	case err == nil:
		s.touchUser(ctx, user, info)
		return user, false, nil
	case !errors.Is(err, domain.ErrNotFound):
		return domain.User{}, false, err
	}

	fresh := s.newUser(info, "")
	fresh.Phone = identity.Value
	created, err := s.repo.CreateUser(ctx, fresh)
	if err != nil {
		return domain.User{}, false, err
	}
	if err := s.repo.SaveIdentity(ctx, created.ID, identity.Kind, identity.Value, identity.Country); err != nil {
		return domain.User{}, false, err
	}
	if err := s.provision(ctx, created, info); err != nil {
		return domain.User{}, false, err
	}
	return created, true, nil
}

// newUser — жаңа тіркелгінің бастапқы мәндері (клиент жіберген метадерек бойынша).
func (s *Service) newUser(info DeviceInfo, address string) domain.User {
	return domain.User{
		Status:     domain.UserActive,
		Email:      address,
		Locale:     domain.NormalizeLocale(info.Locale),
		Timezone:   info.Timezone,
		Platform:   info.Platform,
		AppVersion: info.AppVersion,
		OSVersion:  info.OSVersion,
	}
}

// touchUser — платформа, нұсқа, тіл сияқты метадеректі жаңартады. Сәтсіздігі кіруге кедергі емес.
func (s *Service) touchUser(ctx context.Context, user domain.User, info DeviceInfo) {
	_ = s.repo.UpdateUserMeta(ctx, user.ID, info.Platform, info.AppVersion, info.OSVersion,
		domain.NormalizeLocale(info.Locale), info.Timezone)
}

// provision — жаңа тіркелгіге бастапқы тариф береді және нұсқаны статистикаға жазады.
func (s *Service) provision(ctx context.Context, user domain.User, info DeviceInfo) error {
	if s.prov != nil {
		if err := s.prov.EnsureSubscription(ctx, user.ID); err != nil {
			return err
		}
	}
	_ = s.repo.RecordAppVersion(ctx, info.Platform, info.AppVersion, "")
	return nil
}

// openSession — өшірілген тіркелгіні тоқтатады, әйтпесе токендер жұбын береді.
func (s *Service) openSession(ctx context.Context, user domain.User, isNew bool, info DeviceInfo) (Session, error) {
	if user.Status == domain.UserDisabled {
		return Session{}, domain.ErrAccountDisabled
	}
	session, err := s.issueSession(ctx, user, info, "")
	if err != nil {
		return Session{}, err
	}
	session.IsNewUser = isNew
	return session, nil
}

// issueSession — құрылғыны тіркеп, токендер жұбын береді.
func (s *Service) issueSession(ctx context.Context, user domain.User, info DeviceInfo, family string) (Session, error) {
	device, err := s.repo.UpsertDevice(ctx, domain.Device{
		ID:         info.DeviceID,
		UserID:     user.ID,
		Platform:   info.Platform,
		AppVersion: info.AppVersion,
		OSVersion:  info.OSVersion,
		Model:      info.Model,
		Locale:     domain.NormalizeLocale(info.Locale),
	})
	if err != nil {
		return Session{}, err
	}

	access, expiresIn, err := s.accessToken(user, device.ID)
	if err != nil {
		return Session{}, err
	}

	raw := traits.RandomToken(32)
	expiresAt := s.clock.Now().Add(s.cfg.RefreshTTL)
	record := repository.RefreshToken{
		UserID:    user.ID,
		DeviceID:  device.ID,
		FamilyID:  family,
		TokenHash: HashToken(s.cfg.RefreshSecret, raw),
		ExpiresAt: expiresAt,
		UserAgent: traits.Clamp(info.UserAgent, 200),
	}
	if _, err := s.repo.CreateRefreshToken(ctx, record); err != nil {
		return Session{}, err
	}
	_ = s.repo.TouchUser(ctx, user.ID)

	return Session{
		User:             user,
		AccessToken:      access,
		RefreshToken:     raw,
		AccessExpiresIn:  expiresIn,
		RefreshExpiresAt: expiresAt,
		DeviceID:         device.ID,
	}, nil
}

func (s *Service) accessToken(user domain.User, deviceID string) (string, int, error) {
	now := s.clock.Now()
	claims := Claims{
		Subject:   user.ID,
		Issuer:    s.cfg.Issuer,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(s.cfg.AccessTTL).Unix(),
		TokenID:   traits.NewID(),
		Type:      "access",
		DeviceID:  deviceID,
		Platform:  user.Platform,
	}
	token, err := SignJWT(s.cfg.AccessSecret, claims)
	return token, int(s.cfg.AccessTTL.Seconds()), err
}

// Refresh — токенді ротациялау. Қайта пайдалану анықталса, бүкіл тізбек жабылады.
func (s *Service) Refresh(ctx context.Context, rawToken string, info DeviceInfo) (Session, error) {
	if strings.TrimSpace(rawToken) == "" {
		return Session{}, domain.ErrUnauthorized
	}
	hash := HashToken(s.cfg.RefreshSecret, rawToken)
	stored, err := s.repo.RefreshTokenByHash(ctx, hash)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, domain.ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}

	now := s.clock.Now()
	if stored.RevokedAt != nil {
		// Жабылған токенді қайта қолдану — ұрлану белгісі: тізбекті толық жабамыз.
		_ = s.repo.RevokeFamily(ctx, stored.FamilyID, "reuse_detected")
		s.log.Warn("refresh token reuse detected", "user_id", stored.UserID, "family", stored.FamilyID)
		return Session{}, domain.ErrUnauthorized
	}
	if now.After(stored.ExpiresAt) {
		return Session{}, domain.ErrUnauthorized
	}

	user, err := s.repo.UserByID(ctx, stored.UserID)
	if err != nil {
		return Session{}, domain.ErrUnauthorized
	}
	if user.Status == domain.UserDisabled {
		return Session{}, domain.ErrAccountDisabled
	}

	raw := traits.RandomToken(32)
	next := repository.RefreshToken{
		UserID:    user.ID,
		DeviceID:  stored.DeviceID,
		FamilyID:  stored.FamilyID,
		TokenHash: HashToken(s.cfg.RefreshSecret, raw),
		ExpiresAt: now.Add(s.cfg.RefreshTTL),
		UserAgent: traits.Clamp(info.UserAgent, 200),
	}
	if _, err := s.repo.RotateRefreshToken(ctx, stored.ID, next); err != nil {
		return Session{}, domain.ErrUnauthorized
	}

	access, expiresIn, err := s.accessToken(user, stored.DeviceID)
	if err != nil {
		return Session{}, err
	}
	_ = s.repo.TouchUser(ctx, user.ID)

	return Session{
		User:             user,
		AccessToken:      access,
		RefreshToken:     raw,
		AccessExpiresIn:  expiresIn,
		RefreshExpiresAt: next.ExpiresAt,
		DeviceID:         stored.DeviceID,
	}, nil
}

// LogoutSession — берілген refresh токенді жабады; токен қай тіркелгіге тиесілі болғанын қайтарады ("" — белгісіз).
//
// The account id lets the caller detach the app installation from that
// account in the same request, so no push meant for it reaches the phone
// after sign-out.
func (s *Service) LogoutSession(ctx context.Context, rawToken string) (string, error) {
	if strings.TrimSpace(rawToken) == "" {
		return "", nil
	}
	stored, err := s.repo.RefreshTokenByHash(ctx, HashToken(s.cfg.RefreshSecret, rawToken))
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil // идемпотентті
	}
	if err != nil {
		return "", err
	}
	return stored.UserID, s.repo.RevokeRefreshToken(ctx, stored.ID, "logout")
}

// Authenticate — access токенді тексеріп, қолданушыны қайтарады.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, Claims, error) {
	claims, err := ParseJWT(s.cfg.AccessSecret, token, s.clock.Now())
	if err != nil {
		return domain.User{}, Claims{}, domain.ErrUnauthorized
	}
	if claims.Type != "access" {
		return domain.User{}, Claims{}, domain.ErrUnauthorized
	}
	user, err := s.repo.UserByID(ctx, claims.Subject)
	if err != nil {
		return domain.User{}, Claims{}, domain.ErrUnauthorized
	}
	if user.Status == domain.UserDisabled {
		return domain.User{}, Claims{}, domain.ErrAccountDisabled
	}
	return user, claims, nil
}

// LegacyRegister — ескі мобильді build-тер үшін install_id → токен.
//
// Kept so the currently shipped iOS and Android builds keep working against
// this server unchanged. The install id is never stored: only an HMAC of it.
func (s *Service) LegacyRegister(ctx context.Context, installID string) (string, time.Time, error) {
	if len(installID) < 16 || len(installID) > 128 {
		return "", time.Time{}, domain.ErrInvalidRequest
	}
	clientID := LegacyClientID(s.cfg.LegacySecret, installID)

	user, err := s.repo.UserByLegacyClient(ctx, clientID)
	if errors.Is(err, domain.ErrNotFound) {
		user, err = s.repo.CreateUser(ctx, domain.User{
			Status:       domain.UserActive,
			Kind:         "legacy_install",
			Platform:     domain.PlatformLegacy,
			LegacyClient: clientID,
		})
		if err != nil {
			return "", time.Time{}, err
		}
		if s.prov != nil {
			if err := s.prov.EnsureSubscription(ctx, user.ID); err != nil {
				return "", time.Time{}, err
			}
		}
	} else if err != nil {
		return "", time.Time{}, err
	}

	now := s.clock.Now()
	expires := now.Add(s.cfg.RefreshTTL)
	claims := Claims{
		Subject:   user.ID,
		Issuer:    s.cfg.Issuer,
		IssuedAt:  now.Unix(),
		ExpiresAt: expires.Unix(),
		TokenID:   traits.NewID(),
		Type:      "legacy",
		Platform:  domain.PlatformLegacy,
	}
	token, err := SignJWT(s.cfg.LegacySecret, claims)
	return token, expires, err
}

// AuthenticateLegacy — ескі токенді тексеру.
func (s *Service) AuthenticateLegacy(ctx context.Context, token string) (domain.User, error) {
	claims, err := ParseJWT(s.cfg.LegacySecret, token, s.clock.Now())
	if err != nil || claims.Type != "legacy" {
		return domain.User{}, domain.ErrUnauthorized
	}
	user, err := s.repo.UserByID(ctx, claims.Subject)
	if err != nil {
		return domain.User{}, domain.ErrUnauthorized
	}
	if user.Status == domain.UserDisabled {
		return domain.User{}, domain.ErrAccountDisabled
	}
	return user, nil
}
