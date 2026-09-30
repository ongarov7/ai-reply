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
// One interface, one demo implementation. A real acquirer is a second
// implementation of exactly these four methods — business code never learns
// which one is wired in.
type Provider interface {
	Name() string
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

// Events — сәтті төлемнен кейінгі оқиғалар (мысалы, push хабарламасы).
//
// Payments do not know how the person is told: the notification layer
// implements this. It must not fail the payment, so it returns nothing.
type Events interface {
	PaymentSucceeded(ctx context.Context, p domain.Payment, sub domain.Subscription)
}

// Service — төлем сценарийлері.
type Service struct {
	repo     *repository.Store
	subs     *subscriptions.Service
	provider Provider
	mode     string
	events   Events
}

// WithEvents — оқиға тыңдаушысы.
func (s *Service) WithEvents(e Events) *Service { s.events = e; return s }

// New — қызмет.
func New(repo *repository.Store, subs *subscriptions.Service, provider Provider, mode string) *Service {
	return &Service{repo: repo, subs: subs, provider: provider, mode: mode}
}

// Mode — demo немесе live.
func (s *Service) Mode() string { return s.mode }

// Start — таңдалған тарифке төлем бастау.
func (s *Service) Start(ctx context.Context, userID, planID string) (Intent, error) {
	plan, err := s.repo.Plan(ctx, planID)
	if err != nil {
		return Intent{}, err
	}
	if plan.IsFree {
		return Intent{}, domain.ErrInvalidRequest
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
	if err == nil && s.events != nil {
		payment.Status = "succeeded"
		s.events.PaymentSucceeded(ctx, payment, sub)
	}
	return sub, err
}

// History — төлемдер тарихы.
func (s *Service) History(ctx context.Context, userID string) ([]domain.Payment, error) {
	return s.repo.PaymentsByUser(ctx, userID, 50)
}
