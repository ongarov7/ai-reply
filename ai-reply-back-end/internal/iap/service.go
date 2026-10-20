// Package iap — қосымша ішіндегі сатып алулар: жауап пакеттері мен pass (App Store, Google Play).
//
// The apps buy through StoreKit 2 and Play Billing and post what the store
// gave them; the server verifies it with the store's own signature (Apple JWS)
// or API (Google), maps the store product to what an administrator configured
// and grants the units through one repository transaction. A purchase is
// bound to the account by an account token the app passes to the store
// (appAccountToken / obfuscatedAccountId), so a transaction can never be
// redeemed by another account. Refunds come back as App Store Server
// Notifications and as Google's voided purchases and revoke what is left.
//
// Off until configured: a store integration in the environment, products an
// administrator created, and the iap_enabled switch. Once a purchase is paid
// it is always honoured while the store integration is configured, even if
// sales were switched off or the product archived in between.
package iap

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// SettingEnabled — әкімшінің «сатып алу қосулы» ауыстырғышы (system_settings, 0018_iap).
const SettingEnabled = "iap_enabled"

// Өнім шектері.
var (
	UnitsRange    = Range{Min: 1, Max: 100000}
	DurationRange = Range{Min: 1, Max: 720}
	SortRange     = Range{Min: 0, Max: 10000}
)

// NameMax — өнім атауының шегі (таңбамен).
const NameMax = 60

// Range — рұқсат етілген ауқым (шеттерімен қоса).
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

func (r Range) has(v int) bool { return v >= r.Min && v <= r.Max }

var (
	codePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{1,39}$`)
	applePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	googlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._]{0,99}$`)
	// tokenPattern — Google purchase token: URL-қауіпсіз мәтін (ұзындығы validToken-да).
	tokenPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// validToken — Google purchase token пішіні: 8…1024 таңба, тек URL-қауіпсіз.
func validToken(token string) bool {
	return len(token) >= 8 && len(token) <= 1024 && tokenPattern.MatchString(token)
}

// Play — Google Play Developer API (googleplay.Client; тестте жалғаны).
type Play interface {
	GetProductPurchase(ctx context.Context, productID, token string) (googleplay.ProductPurchase, error)
	Consume(ctx context.Context, productID, token string) error
	VoidedPurchases(ctx context.Context, since time.Time, pageToken string) (googleplay.VoidedPage, error)
}

// Deps — қызмет тәуелділіктері.
type Deps struct {
	Repo   *repository.Store
	Config config.IAP
	// Secret — JWT_ACCESS_SECRET: тіркелгі токені мен шифрланған Google токенінің кілттері осыдан.
	Secret string
	Log    *slog.Logger
}

// Service — сатып алулар.
type Service struct {
	repo   *repository.Store
	cfg    config.IAP
	log    *slog.Logger
	clock  traits.Clock
	ttl    time.Duration
	now    func() time.Time // кэш үшін қабырға сағаты
	apple  *appstore.Verifier
	google Play

	accountKey []byte
	sealer     *sealer

	mu       sync.Mutex
	enabled  bool
	products []domain.IAPProduct
	loadedAt time.Time
	loaded   bool

	pollMu      sync.Mutex
	voidedSince time.Time
	lastPoll    time.Time
}

// New — қызмет. Бапталған дүкендердің тексерушілері осы жерде жасалады (сыртқа сұраныс жоқ).
func New(d Deps) (*Service, error) {
	if len(d.Secret) < 32 {
		return nil, errors.New("iap: the server secret is required")
	}
	s := &Service{
		repo: d.Repo, cfg: d.Config, log: d.Log, clock: traits.SystemClock{}, ttl: 5 * time.Second, now: time.Now,
		accountKey: deriveKey(d.Secret, "ai-reply/iap-account-token/v1"),
		sealer:     newSealer(deriveKey(d.Secret, "ai-reply/iap-purchase-token/v1")),
	}
	if d.Config.Apple() {
		v, err := appstore.New()
		if err != nil {
			return nil, fmt.Errorf("iap: %w", err)
		}
		s.apple = v
	}
	if d.Config.Google() {
		c, err := googleplay.New(googleplay.Config{
			PackageName: d.Config.GooglePackageName, ClientEmail: d.Config.GoogleClientEmail,
			PrivateKey: d.Config.GooglePrivateKey,
		})
		if err != nil {
			return nil, fmt.Errorf("iap: %w", err)
		}
		s.google = c
	}
	return s, nil
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// WithTTL — баптау кэшінің ұзақтығы (тестте 0).
func (s *Service) WithTTL(ttl time.Duration) *Service { s.ttl = ttl; return s }

// WithAppleVerifier — тестке: жалған түбірге сенетін тексеруші (nil — App Store өшірулі).
func (s *Service) WithAppleVerifier(v *appstore.Verifier) *Service { s.apple = v; return s }

// WithGooglePlay — тестке: жалған серверге бағытталған клиент (nil — Google Play өшірулі).
func (s *Service) WithGooglePlay(p Play) *Service {
	s.google = p
	return s
}

// ---------------------------------------------------------------- availability

// Configured — бұл платформаның дүкен интеграциясы серверде бар ма.
func (s *Service) Configured(platform string) bool {
	switch platform {
	case domain.PlatformIOS:
		return s.apple != nil
	case domain.PlatformAndroid:
		return s.google != nil
	}
	return false
}

// AnyConfigured — кемінде бір дүкен бапталған.
func (s *Service) AnyConfigured() bool { return s.apple != nil || s.google != nil }

// Status — әкімші бетіне: не бапталған (құпиясыз).
type Status struct {
	Apple        bool
	Google       bool
	AppleSandbox bool
	// GoogleTest — Google лицензия тестерлерінің сатып алулары қабылданады.
	GoogleTest    bool
	BundleID      string
	PackageName   string
	VoidedPolling bool
}

// Status — интеграциялардың күйі.
func (s *Service) Status() Status {
	return Status{Apple: s.apple != nil, Google: s.google != nil, AppleSandbox: s.cfg.AppleAcceptSandbox,
		GoogleTest: s.cfg.GoogleAcceptTest, BundleID: s.cfg.AppleBundleID, PackageName: s.cfg.GooglePackageName,
		VoidedPolling: s.google != nil && s.cfg.GoogleVoidedPoll > 0}
}

// state — ауыстырғыш және өнімдер (5 секунд кэште, limits.Service сияқты).
//
// A failed read keeps the last known state; before the first successful
// read it answers "off", so a database hiccup can never open sales.
func (s *Service) state(ctx context.Context) (bool, []domain.IAPProduct) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded && s.now().Sub(s.loadedAt) < s.ttl {
		return s.enabled, s.products
	}
	value, err := s.repo.Setting(ctx, SettingEnabled)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		s.log.Error("iap settings read failed", "error", err.Error())
		return s.loaded && s.enabled, s.products
	}
	products, err := s.repo.IAPProducts(ctx, false)
	if err != nil {
		s.log.Error("iap products read failed", "error", err.Error())
		return s.loaded && s.enabled, s.products
	}
	s.enabled, s.products, s.loadedAt, s.loaded = value == "true", products, s.now(), true
	return s.enabled, s.products
}

func (s *Service) forget() {
	s.mu.Lock()
	s.loaded = false
	s.mu.Unlock()
}

// Enabled — әкімші сатуды қосқан ба.
func (s *Service) Enabled(ctx context.Context) bool {
	enabled, _ := s.state(ctx)
	return enabled
}

// Available — бұл платформада сатып алуға бола ма: интеграция, ауыстырғыш және кемінде бір өнім.
func (s *Service) Available(ctx context.Context, platform string) bool {
	if !s.Configured(platform) {
		return false
	}
	enabled, products := s.state(ctx)
	if !enabled {
		return false
	}
	for _, p := range products {
		if p.Offered(platform) {
			return true
		}
	}
	return false
}

// CanBuy — лимит қатесінің details.can_buy мәні (api.PurchaseOptions).
func (s *Service) CanBuy(ctx context.Context, _ domain.User, platform string) bool {
	return s.Available(ctx, platform)
}

// Products — бұл платформада сатылатын өнімдер (sort_order бойынша).
func (s *Service) Products(ctx context.Context, platform string) ([]domain.IAPProduct, error) {
	if !s.Available(ctx, platform) {
		return nil, domain.ErrIAPUnavailable
	}
	_, all := s.state(ctx)
	out := make([]domain.IAPProduct, 0, len(all))
	for _, p := range all {
		if p.Offered(platform) {
			out = append(out, p)
		}
	}
	return out, nil
}

// SetEnabled — әкімшінің ауыстырғышы. Ешбір дүкен бапталмаса қосуға болмайды (ErrIAPUnavailable).
func (s *Service) SetEnabled(ctx context.Context, enabled bool) (before bool, err error) {
	if enabled && !s.AnyConfigured() {
		return false, domain.ErrIAPUnavailable
	}
	value, err := s.repo.Setting(ctx, SettingEnabled)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return false, err
	}
	next := "false"
	if enabled {
		next = "true"
	}
	if err := s.repo.SetSetting(ctx, SettingEnabled, next); err != nil {
		return false, err
	}
	s.forget()
	return value == "true", nil
}

// AccountToken — тіркелгінің тұрақты UUID-і: дүкенге беріледі (appAccountToken, obfuscatedAccountId).
//
// HMAC of the user id under a key derived from the server secret, formatted
// as a version 4 UUID: stable across devices and reinstalls, says nothing
// about the account, and cannot be computed by anyone else. Rotating
// JWT_ACCESS_SECRET changes every token: purchases made before and posted
// after the rotation are then refused (account_mismatch).
func (s *Service) AccountToken(userID string) string {
	mac := hmac.New(sha256.New, s.accountKey)
	mac.Write([]byte("account|" + userID))
	b := mac.Sum(nil)[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func deriveKey(secret, label string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// tokenHash — Google purchase token-нің SHA-256 hex-і (transaction_id).
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- results

// Нәтиже күйлері (POST /iap/... жауабының status өрісі).
const (
	StatusGranted        = "granted"
	StatusAlreadyGranted = "already_granted"
	StatusPending        = "pending"
)

// Result — тексерілген сатып алу.
type Result struct {
	Status   string
	Purchase domain.IAPPurchase
}

// grant — бір транзакция, бір кредит (repository.GrantPurchase).
func (s *Service) grant(ctx context.Context, user domain.User, product domain.IAPProduct, p domain.IAPPurchase,
	sealed string) (Result, error) {
	quantity := max(p.Quantity, 1)
	p.UserID, p.ProductCode, p.Quantity, p.Units = user.ID, product.Code, quantity, product.Units*quantity
	duration := time.Duration(0)
	if product.Kind == domain.ProductPass {
		duration = time.Duration(product.DurationHours*quantity) * time.Hour
	}
	// The grant runs detached: a client that disconnects must not leave a paid
	// purchase half-recorded (the transaction itself is all or nothing).
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	res, err := s.repo.GrantPurchase(writeCtx, repository.PurchaseGrant{
		Purchase: p, Kind: product.Kind, Duration: duration, SealedToken: sealed, Now: s.clock.Now(),
	})
	if err != nil {
		if errors.Is(err, domain.ErrPurchaseOwnedElsewhere) {
			s.log.Warn("iap purchase belongs to another account", "user_id", user.ID, "provider", p.Provider,
				"product", product.Code)
		}
		return Result{}, err
	}
	status := StatusAlreadyGranted
	if res.Granted {
		status = StatusGranted
		s.log.Info("iap purchase granted", "user_id", user.ID, "provider", p.Provider, "product", product.Code,
			"kind", product.Kind, "units", res.Purchase.Units, "environment", p.Environment, "purchase_id", res.Purchase.ID)
	}
	return Result{Status: status, Purchase: res.Purchase}, nil
}

// ---------------------------------------------------------------- admin

// ProductInput — әкімші жіберген өнім.
type ProductInput struct {
	Code            string
	Kind            string
	Units           int
	DurationHours   int
	AppleProductID  string
	GoogleProductID string
	Name            map[string]string
	IsActive        bool
	SortOrder       int
}

// AdminProducts — барлық өнім (мұрағаттағылары соңында) және әрқайсысының берілген сатылымы.
func (s *Service) AdminProducts(ctx context.Context) ([]domain.IAPProduct, map[string]int, error) {
	products, err := s.repo.IAPProducts(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	sales, err := s.repo.IAPProductSales(ctx)
	return products, sales, err
}

// validate — өнім ережелері. Белсенді өнімнің бірлігі және кемінде бір дүкен id-і болуы керек.
func validate(in ProductInput) (domain.IAPProduct, error) {
	p := domain.IAPProduct{
		Code: strings.TrimSpace(in.Code), Kind: strings.TrimSpace(in.Kind), Units: in.Units,
		DurationHours: in.DurationHours, AppleProductID: strings.TrimSpace(in.AppleProductID),
		GoogleProductID: strings.TrimSpace(in.GoogleProductID), Name: map[string]string{},
		IsActive: in.IsActive, SortOrder: in.SortOrder,
	}
	switch {
	case !codePattern.MatchString(p.Code):
		return p, domain.InvalidField("code", "format")
	case !traits.OneOf(p.Kind, domain.IAPProductKinds...):
		return p, domain.InvalidField("kind", "value")
	case !UnitsRange.has(p.Units):
		return p, domain.InvalidField("units", "range")
	case p.Kind == domain.ProductPass && !DurationRange.has(p.DurationHours):
		return p, domain.InvalidField("duration_hours", "range")
	case p.Kind == domain.ProductPack && p.DurationHours != 0:
		return p, domain.InvalidField("duration_hours", "pass_only")
	case p.AppleProductID != "" && !applePattern.MatchString(p.AppleProductID):
		return p, domain.InvalidField("apple_product_id", "format")
	case p.GoogleProductID != "" && !googlePattern.MatchString(p.GoogleProductID):
		return p, domain.InvalidField("google_product_id", "format")
	case !SortRange.has(p.SortOrder):
		return p, domain.InvalidField("sort_order", "range")
	case p.IsActive && p.AppleProductID == "" && p.GoogleProductID == "":
		return p, domain.InvalidField("is_active", "store_id_required")
	}
	for _, locale := range domain.Locales {
		name := traits.CollapseSpaces(strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, in.Name[locale]))
		if traits.RuneLen(name) > NameMax {
			return p, domain.InvalidField("name."+locale, "too_long")
		}
		p.Name[locale] = name
	}
	return p, nil
}

// CreateProduct — жаңа өнім (бірегей код пен дүкен id-і: ConflictField).
func (s *Service) CreateProduct(ctx context.Context, in ProductInput) (domain.IAPProduct, error) {
	p, err := validate(in)
	if err != nil {
		return domain.IAPProduct{}, err
	}
	created, err := s.repo.CreateIAPProduct(ctx, p, s.clock.Now())
	if err != nil {
		return domain.IAPProduct{}, err
	}
	s.forget()
	return created, nil
}

// UpdateProduct — өнімді өзгерту. Код пен түр өзгермейді: сатып алулар соларға сілтейді.
//
// Units and duration apply to purchases made after the change; what was
// granted stays as it was.
func (s *Service) UpdateProduct(ctx context.Context, id string, in ProductInput) (before, after domain.IAPProduct, err error) {
	before, err = s.repo.IAPProduct(ctx, id)
	if err != nil {
		return before, after, err
	}
	if before.ArchivedAt != nil {
		return before, after, domain.ConflictField("product", "archived")
	}
	if in.Code == "" {
		in.Code = before.Code
	}
	if in.Kind == "" {
		in.Kind = before.Kind
	}
	if strings.TrimSpace(in.Code) != before.Code {
		return before, after, domain.InvalidField("code", "immutable")
	}
	if strings.TrimSpace(in.Kind) != before.Kind {
		return before, after, domain.InvalidField("kind", "immutable")
	}
	after, err = validate(in)
	if err != nil {
		return before, after, err
	}
	after.ID, after.CreatedAt = before.ID, before.CreatedAt
	now := s.clock.Now()
	if err := s.repo.UpdateIAPProduct(ctx, after, now); err != nil {
		return before, after, err
	}
	after.UpdatedAt = now
	s.forget()
	return before, after, nil
}

// ArchiveProduct — өнімді мұрағаттау (сатылмайды, бұрынғы сатып алулар өз өнімін табады).
func (s *Service) ArchiveProduct(ctx context.Context, id string) (domain.IAPProduct, error) {
	p, err := s.repo.IAPProduct(ctx, id)
	if err != nil {
		return p, err
	}
	if p.ArchivedAt != nil {
		return p, domain.ConflictField("product", "archived")
	}
	if err := s.repo.ArchiveIAPProduct(ctx, id, s.clock.Now()); err != nil {
		return p, err
	}
	s.forget()
	return p, nil
}

// Purchases — әкімшінің сатып алулар тізімі.
func (s *Service) Purchases(ctx context.Context, f repository.PurchaseFilter) ([]repository.PurchaseRow, int, error) {
	return s.repo.ListIAPPurchases(ctx, f)
}

// UserPurchases — бір қолданушының сатып алулары (әкімші картасы).
func (s *Service) UserPurchases(ctx context.Context, userID string, limit int) ([]domain.IAPPurchase, error) {
	return s.repo.IAPPurchasesByUser(ctx, userID, limit)
}

// Stats — дүкен, орта және күй бойынша топтар (Sandbox пен тест сатылымы бөлек).
func (s *Service) Stats(ctx context.Context) ([]repository.PurchaseStat, error) {
	return s.repo.IAPPurchaseStats(ctx)
}
