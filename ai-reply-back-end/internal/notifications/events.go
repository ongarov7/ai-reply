package notifications

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Translator — локализация бумасы (localization.Bundle.T).
type Translator interface {
	T(locale, key string) string
}

// BusinessEvents — бизнес-оқиғаларды push хабарламаларына айналдырады.
//
// Payments and subscriptions know nothing about push: they call these
// methods (payments through its Events interface, subscriptions through the
// periodic sweep). Each event carries its own idempotency key, so a repeated
// call — a retried payment confirmation, the same sweep on two processes —
// sends nothing new. Texts come from the server's locale files, in the
// language of the account.
type BusinessEvents struct {
	svc *Service
	tr  Translator
}

// NewBusinessEvents — оқиғалар.
func NewBusinessEvents(svc *Service, tr Translator) *BusinessEvents {
	return &BusinessEvents{svc: svc, tr: tr}
}

// PaymentSucceeded — төлем расталды, тариф қосылды.
func (b *BusinessEvents) PaymentSucceeded(ctx context.Context, p domain.Payment, sub domain.Subscription) {
	if !b.svc.Ready() {
		return
	}
	user, err := b.svc.repo.UserByID(ctx, p.UserID)
	if err != nil {
		b.svc.log.Warn("payment notification skipped", "user_id", p.UserID, "error", err.Error())
		return
	}
	plan, err := b.svc.repo.Plan(ctx, sub.PlanID)
	if err != nil {
		b.svc.log.Warn("payment notification skipped", "user_id", p.UserID, "error", err.Error())
		return
	}
	locale := user.Locale
	b.notify(ctx, UserNotification{
		UserID:         p.UserID,
		IdempotencyKey: "payment_success:" + p.ID,
		Type:           "payment_success",
		Category:       domain.CategorySubscription,
		Title:          b.tr.T(locale, "push.payment_success.title"),
		Body:           fill(b.tr.T(locale, "push.payment_success.body"), "{plan}", plan.LocalizedName(locale)),
		Link:           "aireply://subscription",
	})
}

// SweepSubscriptions — 3 күн ішінде бітетін және соңғы тәулікте біткен тарифтер туралы.
//
// Runs with the subscription expiry job. The idempotency key contains the
// subscription and its end date, so each reminder goes out once per period
// however often the sweep runs; an extended subscription gets a new reminder
// for its new end date.
func (b *BusinessEvents) SweepSubscriptions(ctx context.Context) {
	if !b.svc.Ready() {
		return
	}
	now := b.svc.clock.Now()
	expiring, err := b.svc.repo.SubscriptionsExpiringBetween(ctx, now, now.Add(72*time.Hour))
	if err != nil {
		b.svc.log.Error("subscription reminder sweep failed", "error", err.Error())
		return
	}
	for _, n := range expiring {
		locale := n.Locale
		b.notify(ctx, UserNotification{
			UserID:         n.UserID,
			IdempotencyKey: "subscription_expiring:" + n.SubscriptionID + ":" + strconv.FormatInt(n.ExpiresAt.UnixMilli(), 10),
			Type:           "subscription_expiring",
			Category:       domain.CategorySubscription,
			Title:          b.tr.T(locale, "push.subscription_expiring.title"),
			Body: fill(fill(b.tr.T(locale, "push.subscription_expiring.body"),
				"{plan}", planName(n.PlanName, locale)), "{date}", n.ExpiresAt.In(b.svc.loc).Format("02.01.2006")),
			Link: "aireply://subscription",
		})
	}
	expired, err := b.svc.repo.SubscriptionsExpiredBetween(ctx, now.Add(-24*time.Hour), now)
	if err != nil {
		b.svc.log.Error("subscription expiry sweep failed", "error", err.Error())
		return
	}
	for _, n := range expired {
		locale := n.Locale
		b.notify(ctx, UserNotification{
			UserID:         n.UserID,
			IdempotencyKey: "subscription_expired:" + n.SubscriptionID + ":" + strconv.FormatInt(n.ExpiresAt.UnixMilli(), 10),
			Type:           "subscription_expired",
			Category:       domain.CategorySubscription,
			Title:          b.tr.T(locale, "push.subscription_expired.title"),
			Body:           fill(b.tr.T(locale, "push.subscription_expired.body"), "{plan}", planName(n.PlanName, locale)),
			Link:           "aireply://subscription",
		})
	}
}

func (b *BusinessEvents) notify(ctx context.Context, n UserNotification) {
	if _, err := b.svc.NotifyUser(ctx, n); err != nil {
		b.svc.log.Error("business notification failed", "type", n.Type, "user_id", n.UserID, "error", err.Error())
	}
}

func fill(template, placeholder, value string) string {
	return strings.ReplaceAll(template, placeholder, value)
}

func planName(names map[string]string, locale string) string {
	if v := names[domain.NormalizeLocale(locale)]; v != "" {
		return v
	}
	return names["en"]
}
