package notifications

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// LinkSubscription — тариф пен квота хабарламалары ашатын экран.
const LinkSubscription = "aireply://subscription"

const (
	// eventTimeout — тариф оқиғасының хабарламасын жазу уақыты (сұранысқа тәуелсіз).
	eventTimeout = 5 * time.Second
	// quotaTimeout — квота хабарламасы AI жауабының жолында жазылады: қысқа.
	quotaTimeout = 2 * time.Second
	// expiringWindow — мерзімі осы уақыт ішінде бітетін тариф туралы еске салу.
	expiringWindow = 72 * time.Hour
	// expiredWindow — соңғы осы уақытта біткен тариф туралы хабар (кешігіп іске қосылған сервер үшін).
	expiredWindow = 24 * time.Hour
	// maxRecentKeys — жадтағы «жақында хабарланды» жиынының шегі; толса тазаланады.
	maxRecentKeys = 50_000
	// dateLayout — хабарлама мәтініндегі күн (қолданба белдеуінде).
	dateLayout = "02.01.2006"
)

// Events — бизнес-оқиғалардан автоматты хабарламалар: тариф қосылды, мерзімі
// бітуге жақын, бітті, квота аяқталуға жақын, бітті.
//
// Payments, the admin panel and the AI gateway know nothing about push or
// e-mail: they call these methods through their own small interfaces. Every
// event carries its own idempotency key, so a repeated call (a payment
// confirmed twice, the same sweep on two processes, two requests racing for
// the last generation) creates nothing new. Texts come from the server's
// locale files in the recipient's language. A failure is logged and never
// returned: the payment, the plan change or the reply has already happened.
type Events struct {
	svc    *Service
	recent recentKeys
}

// NewEvents — svc арқылы жіберетін оқиғалар.
func NewEvents(svc *Service) *Events { return &Events{svc: svc} }

// PaymentSucceeded — төлем расталып, ақылы тариф қосылды (payments.Events).
func (e *Events) PaymentSucceeded(ctx context.Context, p domain.Payment, sub domain.Subscription) {
	if sub.Source != "payment" {
		return
	}
	e.activated(ctx, sub, domain.TypeSubscriptionActivated+":payment:"+p.ID)
}

// PlanAssigned — әкімші ақылы тарифті қосты (admin.Events).
func (e *Events) PlanAssigned(ctx context.Context, sub domain.Subscription) {
	if sub.Source != "admin" {
		return
	}
	e.activated(ctx, sub, domain.TypeSubscriptionActivated+":sub:"+sub.ID)
}

// activated — «тариф қосылды»: push және хат (хат санат өшірулі болса да кетеді: ол
// сатып алуды растайды). Тегін тариф туралы да, мерзімі өтіп кеткен күнмен берілген
// тариф туралы да ештеңе жіберілмейді.
func (e *Events) activated(ctx context.Context, sub domain.Subscription, key string) {
	if !sub.IsUsable(e.svc.clock.Now()) {
		return
	}
	ctx, cancel := detached(ctx, eventTimeout)
	defer cancel()
	plan, err := e.svc.repo.Plan(ctx, sub.PlanID)
	if err != nil {
		e.svc.log.Error("subscription notification skipped", "type", domain.TypeSubscriptionActivated,
			"user_id", sub.UserID, "subscription_id", sub.ID, "error", err.Error())
		return
	}
	if plan.IsFree {
		return
	}
	params := map[string]string{"limit": strconv.Itoa(plan.DailyLimit)}
	if sub.ExpiresAt != nil {
		params["date"] = e.date(*sub.ExpiresAt)
	}
	e.notify(ctx, UserNotification{
		UserID:         sub.UserID,
		IdempotencyKey: key,
		Type:           domain.TypeSubscriptionActivated,
		Category:       domain.CategorySubscription,
		Link:           LinkSubscription,
		TitleKey:       "push.subscription_activated.title",
		BodyKey:        "push.subscription_activated.body",
		Params:         params,
		ParamsFor:      planName(plan.Name),
		Email:          true,
		Transactional:  true,
	})
}

// SweepSubscriptions — 72 сағат ішінде бітетін және соңғы тәулікте біткен ақылы тарифтер туралы.
//
// Runs with the subscription expiry job. The key holds the subscription and
// its end date, so each notice goes out once per period however often the
// sweep runs, and an extended subscription is reminded again for its new date.
func (e *Events) SweepSubscriptions(ctx context.Context) {
	now := e.svc.clock.Now()
	expiring, err := e.svc.repo.SubscriptionsExpiringBetween(ctx, now, now.Add(expiringWindow), domain.TypeSubscriptionExpiring)
	if err != nil {
		e.svc.log.Error("subscription reminder sweep failed", "type", domain.TypeSubscriptionExpiring, "error", err.Error())
	}
	for _, n := range expiring {
		if ctx.Err() != nil {
			return
		}
		e.notify(ctx, e.expiryNotice(domain.TypeSubscriptionExpiring, n))
	}
	expired, err := e.svc.repo.SubscriptionsExpiredBetween(ctx, now.Add(-expiredWindow), now, domain.TypeSubscriptionExpired)
	if err != nil {
		e.svc.log.Error("subscription reminder sweep failed", "type", domain.TypeSubscriptionExpired, "error", err.Error())
	}
	for _, n := range expired {
		if ctx.Err() != nil {
			return
		}
		e.notify(ctx, e.expiryNotice(domain.TypeSubscriptionExpired, n))
	}
}

func (e *Events) expiryNotice(kind string, n repository.SubscriptionNotice) UserNotification {
	return UserNotification{
		UserID:         n.UserID,
		IdempotencyKey: repository.SubscriptionNoticeKey(kind, n.SubscriptionID, n.ExpiresAt),
		Type:           kind,
		Category:       domain.CategorySubscription,
		Link:           LinkSubscription,
		TitleKey:       "push." + kind + ".title",
		BodyKey:        "push." + kind + ".body",
		Params:         map[string]string{"date": e.date(n.ExpiresAt)},
		ParamsFor:      planName(n.PlanName),
	}
}

// QuotaUsed — генерациядан кейінгі квота күйі (ai.QuotaEvents). usage-тегі
// UsedToday/UsedMonth осы сұранысты қоса есептелген; date мен month — квота кілттері.
//
// It sits on the reply path, so it is ordered by cost: plain arithmetic
// decides in the common case that there is nothing to say; a notice already
// handled by this process is skipped from memory; only then one idempotent
// database write with a short timeout. "Low" is 0 < remaining ≤ threshold
// and "exhausted" is remaining = 0, once per day (and once per month when
// the plan has a monthly limit). An admin quota reset does not re-arm the
// notices of the same day.
func (e *Events) QuotaUsed(ctx context.Context, user domain.User, usage domain.Entitlement, date, month string) {
	if user.ID == "" || user.Kind != "account" {
		return
	}
	n, ok := e.quotaNotice(user.ID, usage, date, month)
	if !ok {
		return
	}
	key := repository.UserNotificationKey(user.ID, n.IdempotencyKey)
	if e.recent.has(date, key) {
		return
	}
	ctx, cancel := detached(ctx, quotaTimeout)
	defer cancel()
	if e.notify(ctx, n) {
		e.recent.add(date, key)
	}
}

// quotaNotice — қай квота хабарламасы керек (бір сұранысқа ең көбі біреу):
// айлық бітті → күндік бітті → айлық аз қалды → күндік аз қалды.
func (e *Events) quotaNotice(userID string, usage domain.Entitlement, date, month string) (UserNotification, bool) {
	percent := e.svc.cfg.QuotaLowPercent
	day := quotaLevelOf(usage.DailyLimit, usage.UsedToday, percent)
	mon := quotaLevelOf(usage.MonthlyLimit, usage.UsedMonth, percent)
	switch {
	case mon.level == quotaExhausted:
		return quotaNotification(userID, domain.TypeQuotaExhausted, "month", month, mon), true
	case day.level == quotaExhausted:
		return quotaNotification(userID, domain.TypeQuotaExhausted, "day", date, day), true
	case mon.level == quotaLow:
		return quotaNotification(userID, domain.TypeQuotaLow, "month", month, mon), true
	case day.level == quotaLow:
		return quotaNotification(userID, domain.TypeQuotaLow, "day", date, day), true
	}
	return UserNotification{}, false
}

const (
	quotaPlenty = iota
	quotaLow
	quotaExhausted
)

type quotaLevel struct {
	level, limit, remaining int
}

// quotaLevelOf — бір кезеңнің күйі. Лимиті жоқ (0 — шексіз не бос) кезең туралы хабар жоқ.
func quotaLevelOf(limit, used, percent int) quotaLevel {
	if limit <= 0 {
		return quotaLevel{level: quotaPlenty}
	}
	remaining := max(limit-used, 0)
	switch {
	case remaining == 0:
		return quotaLevel{level: quotaExhausted, limit: limit}
	case remaining <= domain.QuotaLowThreshold(limit, percent):
		return quotaLevel{level: quotaLow, limit: limit, remaining: remaining}
	default:
		return quotaLevel{level: quotaPlenty}
	}
}

// quotaNotification — period "day" не "month"; кілт: "<type>:<period>:<YYYY-MM-DD | YYYY-MM>".
func quotaNotification(userID, kind, period, periodKey string, q quotaLevel) UserNotification {
	body := "push." + kind + ".body"
	if period == "month" {
		body = "push." + kind + ".body_month"
	}
	return UserNotification{
		UserID:         userID,
		IdempotencyKey: kind + ":" + period + ":" + periodKey,
		Type:           kind,
		Category:       domain.CategorySubscription,
		Link:           LinkSubscription,
		TitleKey:       "push." + kind + ".title",
		BodyKey:        body,
		Params:         map[string]string{"limit": strconv.Itoa(q.limit), "remaining": strconv.Itoa(q.remaining)},
	}
}

// notify — хабарламаны жазады; қатені журналға жазып, жалғастырады. true — оқиға өңделді.
func (e *Events) notify(ctx context.Context, n UserNotification) bool {
	if _, err := e.svc.NotifyUser(ctx, n); err != nil {
		e.svc.log.Error("automatic notification failed", "type", n.Type, "user_id", n.UserID,
			"key", n.IdempotencyKey, "error", err.Error())
		return false
	}
	return true
}

func (e *Events) date(t time.Time) string { return t.In(e.svc.loc).Format(dateLayout) }

// planName — тариф атауы хабарлама тілінде (жоқ болса — ағылшынша, онда да жоқ болса — кез келгені).
func planName(names map[string]string) func(locale string) map[string]string {
	return func(locale string) map[string]string {
		p := domain.Plan{Name: names}
		return map[string]string{"plan": p.LocalizedName(locale)}
	}
}

// detached — сұраныс жабылса да бітетін, бірақ уақыты шектеулі контекст.
func detached(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

// recentKeys — осы процесте жақында өңделген квота хабарламалары.
//
// Every quota key holds its day or month, so the set starts over when the
// day changes; a key forgotten that way costs one idempotent database write.
type recentKeys struct {
	mu   sync.Mutex
	day  string
	keys map[string]struct{}
}

func (r *recentKeys) has(day, key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.day != day {
		return false
	}
	_, ok := r.keys[key]
	return ok
}

func (r *recentKeys) add(day, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.day != day || len(r.keys) >= maxRecentKeys {
		r.day, r.keys = day, map[string]struct{}{}
	}
	r.keys[key] = struct{}{}
}
