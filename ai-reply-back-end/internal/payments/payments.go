// Package payments — төлем абстракциясы. Нақты эквайринг кейін қосылады.
package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Intent — төлем бастау нәтижесі.
type Intent struct {
	PaymentID   string
	Provider    string
	Status      string
	Amount      int64
	Currency    string
	RedirectURL string
	Demo        bool
}

// Provider — эквайринг келісімшарты.
//
// One interface, one demo implementation. A real acquirer (App Store, Google
// Play Billing) is a second implementation of these methods — business code
// never learns which one is wired in.
type Provider interface {
	Name() string
	// Live — the provider takes real payments and verifies every one of them
	// with the store or acquirer. Only a live provider can sell in production.
	Live() bool
	CreatePayment(ctx context.Context, p domain.Payment) (Intent, error)
	VerifyPayment(ctx context.Context, p domain.Payment) (string, error)
	HandleWebhook(ctx context.Context, payload []byte) (string, string, error) // paymentID, status
	RefundPayment(ctx context.Context, p domain.Payment) error
}

// ErrNotConfigured — нақты провайдер әлі жоқ.
var ErrNotConfigured = errors.New("payments: provider is not configured")

// DemoProvider — демо режимі: сыртқы жүйе жоқ, бірақ ағын нақты.
type DemoProvider struct{}

// Name — провайдер аты.
func (DemoProvider) Name() string { return "demo" }

// Live — демо ешқашан нақты төлем емес.
func (DemoProvider) Live() bool { return false }

// CreatePayment — бірден «төленді» деп белгілеуге дайын ниет жасайды.
func (DemoProvider) CreatePayment(_ context.Context, p domain.Payment) (Intent, error) {
	return Intent{
		PaymentID: p.ID,
		Provider:  "demo",
		Status:    "pending",
		Amount:    p.Amount,
		Currency:  p.Currency,
		Demo:      true,
	}, nil
}

// VerifyPayment — демо режимінде әрқашан сәтті.
func (DemoProvider) VerifyPayment(context.Context, domain.Payment) (string, error) {
	return "succeeded", nil
}

// HandleWebhook — демо режимінде webhook жоқ.
func (DemoProvider) HandleWebhook(context.Context, []byte) (string, string, error) {
	return "", "", ErrNotConfigured
}

// RefundPayment — демо қайтарым.
func (DemoProvider) RefundPayment(context.Context, domain.Payment) error { return nil }

// Events — сәтті төлемнен кейінгі оқиғалар (хабарлама қабаты іске асырады).
//
// Payments do not know how the person is told. The call must not fail the
// payment, so it returns nothing: the implementation logs its own errors.
type Events interface {
	PaymentSucceeded(ctx context.Context, p domain.Payment, sub domain.Subscription)
}

// SettingPurchasesEnabled — әкімшінің «сатып алу қосулы» ауыстырғышы (system_settings).
const SettingPurchasesEnabled = "purchases_enabled"

// Service — төлем сценарийлері.
type Service struct {
	repo     *repository.Store
	subs     *subscriptions.Service
	provider Provider
	mode     string
	events   Events
	// demoCheckout — the demo provider may sell on this (non-production) server.
	demoCheckout bool
	// production — APP_ENV=production: an unverified payment is never announced.
	production bool
}

// New — қызмет.
func New(repo *repository.Store, subs *subscriptions.Service, provider Provider, mode string) *Service {
	return &Service{repo: repo, subs: subs, provider: provider, mode: mode}
}

// WithEvents — төлем оқиғаларын тыңдаушы (nil — жоқ).
func (s *Service) WithEvents(e Events) *Service { s.events = e; return s }

// WithDemoCheckout — демо провайдерге сатуға рұқсат (тек әзірлеу серверінде).
//
// Config refuses PAYMENT_DEMO_CHECKOUT=true when APP_ENV=production, so a
// production server can only sell through a live provider.
func (s *Service) WithDemoCheckout(enabled bool) *Service { s.demoCheckout = enabled; return s }

// WithProduction — production серверінде тек нақты провайдердің төлемі хабарланады.
func (s *Service) WithProduction(production bool) *Service { s.production = production; return s }

// announces — «тариф қосылды» (push және хат) осы сервердің төлемдеріне жіберіле ме.
//
// Only for money that is real or deliberately simulated: a live provider
// that verifies every payment, or demo checkout switched on for a
// non-production server. Config already refuses demo checkout in
// production; this keeps the rule even if the wiring is ever wrong.
func (s *Service) announces() bool {
	return s.provider.Live() || (s.demoCheckout && !s.production)
}

// Mode — off, demo немесе live.
func (s *Service) Mode() string { return s.mode }

// Provider — қосылған провайдер аты.
func (s *Service) Provider() string { return s.provider.Name() }

// Live — провайдер нақты, тексерілетін төлем қабылдайды.
func (s *Service) Live() bool { return s.provider.Live() }

// CheckoutAvailable — серверде сатуға жарайтын төлем интеграциясы бар ма.
//
// A live, verified provider (PAYMENT_MODE=live), or the demo provider on a
// development server that opted in (PAYMENT_MODE=demo with
// PAYMENT_DEMO_CHECKOUT=true). PAYMENT_MODE=off sells nothing. Without one,
// nothing can be bought, whatever the admin switch or plan visibility say.
func (s *Service) CheckoutAvailable() bool {
	switch s.mode {
	case "live":
		return s.provider.Live()
	case "demo":
		return s.provider.Live() || s.demoCheckout
	default:
		return false
	}
}

// PurchasesEnabled — сатып алу қазір ашық па: интеграция бар және әкімші қосқан.
func (s *Service) PurchasesEnabled(ctx context.Context) bool {
	if !s.CheckoutAvailable() {
		return false
	}
	value, err := s.repo.Setting(ctx, SettingPurchasesEnabled)
	return err == nil && value == "true"
}

// SetPurchasesEnabled — әкімшінің ауыстырғышы. Интеграциясыз қосуға болмайды.
func (s *Service) SetPurchasesEnabled(ctx context.Context, enabled bool) error {
	if enabled && !s.CheckoutAvailable() {
		return domain.ErrPurchasesDisabled
	}
	value := "false"
	if enabled {
		value = "true"
	}
	return s.repo.SetSetting(ctx, SettingPurchasesEnabled, value)
}

// Purchasable — бұл тарифті дәл қазір сатып алуға бола ма.
func (s *Service) Purchasable(ctx context.Context, plan domain.Plan) bool {
	return !plan.IsFree && plan.Listed() && s.PurchasesEnabled(ctx)
}

// Start — таңдалған тарифке төлем бастау.
//
// The server decides, not the app: a hidden, disabled, archived or free plan
// is refused even when an old build or a direct request still offers it.
func (s *Service) Start(ctx context.Context, userID, planID string) (Intent, error) {
	plan, err := s.repo.Plan(ctx, planID)
	if errors.Is(err, domain.ErrNotFound) {
		return Intent{}, domain.ErrPlanUnavailable
	}
	if err != nil {
		return Intent{}, err
	}
	if plan.IsFree || !plan.Listed() {
		return Intent{}, domain.ErrPlanUnavailable
	}
	if !s.PurchasesEnabled(ctx) {
		return Intent{}, domain.ErrPurchasesDisabled
	}
	payment, err := s.repo.CreatePayment(ctx, domain.Payment{
		UserID:   userID,
		PlanID:   plan.ID,
		Provider: s.provider.Name(),
		Amount:   plan.Price,
		Currency: plan.Currency,
		Status:   "created",
	})
	if err != nil {
		return Intent{}, err
	}
	intent, err := s.provider.CreatePayment(ctx, payment)
	if err != nil {
		return Intent{}, err
	}
	if err := s.repo.UpdatePaymentStatus(ctx, payment.ID, intent.Status, intent.PaymentID); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

// Confirm — төлемді растап, жазылымды ауыстырады.
func (s *Service) Confirm(ctx context.Context, userID, paymentID string) (domain.Subscription, error) {
	payment, err := s.repo.Payment(ctx, paymentID)
	if err != nil {
		return domain.Subscription{}, err
	}
	if payment.UserID != userID {
		return domain.Subscription{}, domain.ErrNotFound
	}
	if payment.Status == "succeeded" {
		// Idempotent: a repeated confirm returns the plan the payment already
		// gave, without restarting its period or sending a second notice.
		return s.subs.Current(ctx, userID)
	}
	// Demo money is not real: once buying is switched off, an unfinished demo
	// payment can no longer turn into a plan. A live provider's payment that
	// the store confirms is honoured, because the person has paid.
	if !s.provider.Live() && !s.PurchasesEnabled(ctx) {
		return domain.Subscription{}, domain.ErrPurchasesDisabled
	}
	status, err := s.provider.VerifyPayment(ctx, payment)
	if err != nil {
		return domain.Subscription{}, err
	}
	if status != "succeeded" {
		_ = s.repo.UpdatePaymentStatus(ctx, payment.ID, status, "")
		return domain.Subscription{}, domain.ErrPaymentRequired
	}
	ref := fmt.Sprintf("%s-%s", s.provider.Name(), traits.RandomToken(6))
	if err := s.repo.UpdatePaymentStatus(ctx, payment.ID, "succeeded", ref); err != nil {
		return domain.Subscription{}, err
	}
	var expires *time.Time
	sub, err := s.subs.Assign(ctx, userID, payment.PlanID, "payment", expires)
	if err != nil {
		return domain.Subscription{}, err
	}
	if s.events != nil && s.announces() {
		payment.Status, payment.ProviderRef = "succeeded", ref
		s.events.PaymentSucceeded(ctx, payment, sub)
	}
	return sub, nil
}

// History — төлемдер тарихы.
func (s *Service) History(ctx context.Context, userID string) ([]domain.Payment, error) {
	return s.repo.PaymentsByUser(ctx, userID, 50)
}
