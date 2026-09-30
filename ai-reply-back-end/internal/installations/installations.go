// Package installations — қосымша орнатуларының тізілімі: метадерек, push токені, тіркелгі.
//
// An installation is one install of the app on one device, identified by an
// id the app generates (never a hardware identifier). It exists before
// sign-in (anonymous) and is attached to an account when the app registers it
// with a valid access token. The rules that keep account-specific pushes on
// the right phone live here:
//
//   - the account link comes only from the caller's authentication: an
//     authenticated registration attaches the installation to that user, an
//     anonymous one detaches it (a signed-out app never keeps receiving the
//     previous account's notifications);
//   - a push token belongs to exactly one installation: the device presenting
//     it now owns it, older rows lose it;
//   - the raw token is sealed (AES-256-GCM) before it reaches the database and
//     is opened only by the delivery worker. Logs and the admin panel see a
//     fingerprint of its hash.
package installations

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Registration — қосымша жіберетін күй. Қолданушы идентификаторы мұнда жоқ әдейі.
type Registration struct {
	InstallationID       string
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
	NotificationsEnabled *bool
	Push                 *PushToken
}

// PushToken — FCM registration token не APNs device token.
type PushToken struct {
	Provider    string
	Token       string
	Environment string
}

// Service — орнатулар.
type Service struct {
	repo   *repository.Store
	sealer *Sealer
	log    *slog.Logger
	clock  traits.Clock

	touchMu   sync.Mutex
	touchedAt map[string]time.Time
}

// New — қызмет. secret — токенді шифрлау кілтінің көзі (JWT_ACCESS_SECRET).
func New(repo *repository.Store, secret string, log *slog.Logger) *Service {
	return &Service{
		repo: repo, sealer: NewSealer(secret), log: log, clock: traits.SystemClock{},
		touchedAt: map[string]time.Time{},
	}
}

// WithClock — тестке арналған.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// Sealer — жұмысшыға токенді ашу үшін.
func (s *Service) Sealer() *Sealer { return s.sealer }

var (
	installationIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
	fcmTokenPattern       = regexp.MustCompile(`^[A-Za-z0-9_:.\-]+$`)
	apnsTokenPattern      = regexp.MustCompile(`^[0-9a-fA-F]{64,200}$`)
	shortTextPattern      = regexp.MustCompile(`^[\p{L}\p{N} ._,()+/-]*$`)
)

// ValidInstallationID — қосымша жасаған идентификатор пішімі.
func ValidInstallationID(id string) bool { return installationIDPattern.MatchString(id) }

// Register — орнатуды тіркейді не жаңартады. userID "" — анонимді сұраныс.
func (s *Service) Register(ctx context.Context, reg Registration, userID string) (domain.Installation, error) {
	in, err := s.normalize(reg)
	if err != nil {
		return domain.Installation{}, err
	}
	in.UserID = userID
	in.Now = s.clock.Now()

	inst, change, err := s.repo.UpsertInstallation(ctx, in)
	if err != nil {
		return domain.Installation{}, err
	}
	log := s.log.With("installation", domain.ShortID(inst.InstallationID), "platform", inst.Platform)
	switch {
	case change.Created:
		log.Info("installation registered", "attached", userID != "", "push", inst.TokenFingerprint())
	case change.PreviousUserID != "" && change.PreviousUserID != userID:
		// Detached (sign-out) or handed to another account on the same phone:
		// from now on the previous account's notifications skip this device.
		log.Info("installation owner changed", "previous_user_id", change.PreviousUserID,
			"user_id", userID)
	}
	if change.TokenChanged {
		log.Info("push token updated", "push", inst.TokenFingerprint())
	}
	if change.TokenMoved > 0 {
		log.Info("push token moved from an older installation", "count", change.TokenMoved,
			"push", inst.TokenFingerprint())
	}
	return inst, nil
}

func (s *Service) normalize(reg Registration) (repository.InstallationUpsert, error) {
	id := strings.TrimSpace(reg.InstallationID)
	if !installationIDPattern.MatchString(id) {
		return repository.InstallationUpsert{}, fieldError("installation_id")
	}
	platform := strings.ToLower(strings.TrimSpace(reg.Platform))
	if platform != domain.PlatformIOS && platform != domain.PlatformAndroid {
		return repository.InstallationUpsert{}, fieldError("platform")
	}
	permission := strings.ToLower(strings.TrimSpace(reg.Permission))
	switch permission {
	case "":
		permission = domain.PermissionUnknown
	case domain.PermissionAuthorized, domain.PermissionDenied, domain.PermissionNotDetermined,
		domain.PermissionProvisional, domain.PermissionEphemeral, domain.PermissionUnknown:
	default:
		return repository.InstallationUpsert{}, fieldError("notification_permission")
	}
	enabled := true
	if reg.NotificationsEnabled != nil {
		enabled = *reg.NotificationsEnabled
	}
	osName := reg.OSName
	if osName == "" {
		osName = map[string]string{domain.PlatformIOS: "iOS", domain.PlatformAndroid: "Android"}[platform]
	}
	in := repository.InstallationUpsert{
		InstallationID:       id,
		Platform:             platform,
		AppVersion:           clean(reg.AppVersion, 32),
		AppBuild:             clean(reg.AppBuild, 32),
		OSName:               clean(osName, 32),
		OSVersion:            clean(reg.OSVersion, 32),
		DeviceModel:          clean(reg.DeviceModel, 64),
		Manufacturer:         clean(reg.Manufacturer, 64),
		Locale:               normalizeLocale(reg.Locale),
		Timezone:             clean(reg.Timezone, 64),
		Permission:           permission,
		NotificationsEnabled: enabled,
	}
	if reg.Push != nil && strings.TrimSpace(reg.Push.Token) != "" {
		token := strings.TrimSpace(reg.Push.Token)
		provider := strings.ToLower(strings.TrimSpace(reg.Push.Provider))
		if provider == "" {
			provider = domain.ProviderFor(platform)
		}
		if provider != domain.ProviderFor(platform) {
			return repository.InstallationUpsert{}, fieldError("push.provider")
		}
		env := ""
		switch provider {
		case domain.ProviderFCM:
			if len(token) < 20 || len(token) > 4096 || !fcmTokenPattern.MatchString(token) {
				return repository.InstallationUpsert{}, fieldError("push.token")
			}
		case domain.ProviderAPNs:
			if !apnsTokenPattern.MatchString(token) {
				return repository.InstallationUpsert{}, fieldError("push.token")
			}
			token = strings.ToLower(token)
			switch strings.ToLower(strings.TrimSpace(reg.Push.Environment)) {
			case "", "unknown":
			case "sandbox", "development":
				env = domain.APNsSandbox
			case "production":
				env = domain.APNsProduction
			default:
				return repository.InstallationUpsert{}, fieldError("push.environment")
			}
		}
		sealed, err := s.sealer.Seal(token)
		if err != nil {
			return repository.InstallationUpsert{}, err
		}
		in.HasToken = true
		in.Provider = provider
		in.Environment = env
		in.TokenSealed = sealed
		in.TokenHash = push.TokenHash(token)
	}
	return in, nil
}

// Detach — шығу кезінде: орнату тек осы қолданушыныкі болса ажыратылады.
func (s *Service) Detach(ctx context.Context, installationID, userID string) (bool, error) {
	if !installationIDPattern.MatchString(installationID) || userID == "" {
		return false, nil
	}
	detached, err := s.repo.DetachInstallation(ctx, installationID, userID, s.clock.Now())
	if err == nil && detached {
		s.log.Info("installation detached", "installation", domain.ShortID(installationID), "user_id", userID)
	}
	return detached, err
}

// DetachAny — қосымша «шықтым» дейді, бірақ токені жоқ: кімге тіркелгені маңызды емес.
//
// Detaching is the safe direction (fewer account notifications, never more),
// so it needs no proof of ownership; the app re-attaches with its token on
// the next authenticated registration.
func (s *Service) DetachAny(ctx context.Context, installationID string) (bool, error) {
	if !installationIDPattern.MatchString(installationID) {
		return false, nil
	}
	detached, err := s.repo.DetachInstallationAny(ctx, installationID, s.clock.Now())
	if err == nil && detached {
		s.log.Info("installation detached", "installation", domain.ShortID(installationID), "anonymous", true)
	}
	return detached, err
}

// Lookup — қосымшаның идентификаторы бойынша орнату.
func (s *Service) Lookup(ctx context.Context, installationID string) (domain.Installation, error) {
	return s.repo.InstallationByClientID(ctx, installationID)
}

// DetachUser — барлық сессиясы жабылған не бұғатталған қолданушының құрылғылары.
func (s *Service) DetachUser(ctx context.Context, userID string) (int64, error) {
	return s.repo.DetachUserInstallations(ctx, userID, s.clock.Now())
}

// TouchInterval — last_seen_at жаңартуларының ең аз аралығы.
const TouchInterval = 15 * time.Minute

// Touch — сұраныс тақырыптарынан last_seen_at. Сирек және фонда: API жауабын кешіктірмейді.
func (s *Service) Touch(installationID, appVersion, appBuild, osVersion string) {
	if installationID == "" {
		return
	}
	now := s.clock.Now()
	s.touchMu.Lock()
	if last, ok := s.touchedAt[installationID]; ok && now.Sub(last) < TouchInterval {
		s.touchMu.Unlock()
		return
	}
	if len(s.touchedAt) > 50_000 {
		s.touchedAt = map[string]time.Time{}
	}
	s.touchedAt[installationID] = now
	s.touchMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.TouchInstallation(ctx, repository.InstallationTouch{
			InstallationID: installationID, AppVersion: appVersion, AppBuild: appBuild,
			OSVersion: osVersion, Now: now, MinInterval: TouchInterval,
		}); err != nil {
			s.log.Warn("installation touch failed", "installation", domain.ShortID(installationID), "error", err.Error())
		}
	}()
}

func fieldError(field string) error { return domain.InvalidField(field, "") }

func clean(v string, max int) string {
	v = traits.Clamp(traits.CollapseSpaces(v), max)
	if !shortTextPattern.MatchString(v) {
		return ""
	}
	return v
}

func normalizeLocale(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.IndexAny(v, "-_"); i > 0 {
		v = v[:i]
	}
	if len(v) < 2 || len(v) > 3 {
		return ""
	}
	for _, r := range v {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return v
}

// ---------------------------------------------------------------- sealing

// Sealer — push токенін AES-256-GCM-мен шифрлайды.
//
// The key is derived from a server secret, so a copy of the database file
// alone does not reveal tokens. If that secret is rotated, old tokens stop
// opening: the worker marks those installations invalid and every app
// re-registers its token on the next launch.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer — кілтті құпиядан туындатады.
func NewSealer(secret string) *Sealer {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("ai-reply/push-token/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		panic("installations: aes: " + err.Error())
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic("installations: gcm: " + err.Error())
	}
	return &Sealer{aead: aead}
}

const sealPrefix = "v1:"

// ErrUnseal — токен ашылмады (кілт ауысқан не жол бүлінген).
var ErrUnseal = errors.New("installations: push token cannot be opened")

// Seal — шифрлау.
func (s *Sealer) Seal(token string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("installations: nonce: %w", err)
	}
	out := s.aead.Seal(nonce, nonce, []byte(token), []byte(sealPrefix))
	return sealPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// Open — ашу.
func (s *Sealer) Open(sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealPrefix) {
		return "", ErrUnseal
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", ErrUnseal
	}
	nonce, body := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, body, []byte(sealPrefix))
	if err != nil {
		return "", ErrUnseal
	}
	return string(plain), nil
}
