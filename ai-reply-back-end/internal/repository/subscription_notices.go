package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// UserNotificationKey — бір қолданушының бір оқиғасының dedupe кілті (notifications.dedupe_key).
func UserNotificationKey(userID, key string) string { return "user:" + userID + ":" + key }

// SubscriptionNoticeKey — бір жазылымның бір мерзіміне бір хабарлама:
// "<type>:<subscription id>:<expires_at ms>". An extended subscription has a
// new end date and so gets a new notice.
func SubscriptionNoticeKey(kind, subscriptionID string, expiresAt time.Time) string {
	return kind + ":" + subscriptionID + ":" + strconv.FormatInt(ms(expiresAt), 10)
}

// noticeSent — осы жазылымның осы мерзіміне хабарлама жасалған (бір аргумент: түр).
// The same layout as UserNotificationKey(user, SubscriptionNoticeKey(...)).
const noticeSent = `EXISTS (SELECT 1 FROM notifications n
	WHERE n.dedupe_key = 'user:' || s.user_id || ':' || ? || ':' || s.id || ':' || s.expires_at)`

// SubscriptionNotice — тариф мерзімі туралы хабарламаға қажет дерек.
type SubscriptionNotice struct {
	SubscriptionID string
	UserID         string
	PlanName       map[string]string // locale → name
	ExpiresAt      time.Time
}

// maxSubscriptionNotices — бір өтудегі шек; қалғаны келесі өтуде алынады.
const maxSubscriptionNotices = 500

func (s *Store) subscriptionNotices(ctx context.Context, query string, args ...any) ([]SubscriptionNotice, error) {
	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubscriptionNotice
	for rows.Next() {
		var (
			n              SubscriptionNotice
			expires        int64
			kk, ru, en, uz string
		)
		if err := rows.Scan(&n.SubscriptionID, &n.UserID, &expires, &kk, &ru, &en, &uz); err != nil {
			return nil, err
		}
		n.ExpiresAt = timeFrom(expires)
		n.PlanName = map[string]string{"kk": kk, "ru": ru, "en": en, "uz": uz}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SubscriptionsExpiringBetween — ақылы, жарамды, мерзімі (from, to] аралығында бітетін және
// kind түріндегі хабарламасы әлі жасалмаған жазылымдар.
//
// Only ordinary active accounts: the simulator and legacy install tokens are
// never reminded. Already notified rows are filtered here, so a sweep that
// finds nothing new costs one indexed query, and a long list is worked
// through over consecutive sweeps.
func (s *Store) SubscriptionsExpiringBetween(ctx context.Context, from, to time.Time, kind string) ([]SubscriptionNotice, error) {
	return s.subscriptionNotices(ctx, `
		SELECT s.id, s.user_id, s.expires_at, p.name_kk, p.name_ru, p.name_en, p.name_uz
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id JOIN users u ON u.id = s.user_id
		WHERE s.expires_at > ? AND s.expires_at <= ? AND s.status IN ('active','trial') AND p.is_free = 0
		  AND u.status = 'active' AND u.kind = 'account' AND u.deleted_at IS NULL
		  AND NOT `+noticeSent+`
		ORDER BY s.expires_at LIMIT ?`, ms(from), ms(to), kind, maxSubscriptionNotices)
}

// ActivationNotice — «тариф қосылды» хабарламасы жоқ жазылым және оның кілтінің соңы:
// "payment:<payment id>" (төлем) не "sub:<subscription id>" (әкімші).
type ActivationNotice struct {
	Subscription domain.Subscription
	Ref          string
}

// SubscriptionsMissingActivation — since-тен бері төлеммен не әкімші берген, әлі жарамды
// ақылы жазылымдар, олардың kind хабарламасы жоқ болса.
//
// A payment subscription is matched to the succeeded payment that created
// it (same person and plan, confirmed just before), because its notice is
// keyed by the payment. Demo payments count only when demoPayments is true,
// exactly as they are announced at confirmation.
func (s *Store) SubscriptionsMissingActivation(ctx context.Context, since, now time.Time, kind string, demoPayments bool) ([]ActivationNotice, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		WITH candidates AS (
			SELECT s.id, s.user_id, s.plan_id, s.status, s.source, s.started_at, s.expires_at, s.cancelled_at,
			       s.created_at, s.updated_at,
			       CASE s.source WHEN 'admin' THEN 'sub:' || s.id ELSE (
			           SELECT 'payment:' || pay.id FROM payments pay
			           WHERE pay.user_id = s.user_id AND pay.plan_id = s.plan_id AND pay.status = 'succeeded'
			             AND pay.updated_at <= s.created_at AND (pay.provider <> 'demo' OR ?)
			           ORDER BY pay.updated_at DESC LIMIT 1) END AS ref
			FROM subscriptions s JOIN plans p ON p.id = s.plan_id JOIN users u ON u.id = s.user_id
			WHERE s.source IN ('payment','admin') AND s.created_at >= ? AND s.status IN ('active','trial')
			  AND (s.expires_at IS NULL OR s.expires_at > ?) AND p.is_free = 0
			  AND u.status = 'active' AND u.kind = 'account' AND u.deleted_at IS NULL)
		SELECT `+subColumns+`, ref FROM candidates c
		WHERE ref IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.dedupe_key = 'user:' || c.user_id || ':' || ? || ':' || c.ref)
		ORDER BY created_at LIMIT ?`, demoPayments, ms(since), ms(now), kind, maxSubscriptionNotices)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivationNotice
	for rows.Next() {
		var n ActivationNotice
		sub, err := scanSubscription(scanner(func(dest ...any) error {
			return rows.Scan(append(dest, &n.Ref)...)
		}))
		if err != nil {
			return nil, err
		}
		n.Subscription = sub
		out = append(out, n)
	}
	return out, rows.Err()
}

// scanner — Scan функциясын scanSubscription қабылдайтын түрге айналдырады.
type scanner func(dest ...any) error

func (f scanner) Scan(dest ...any) error { return f(dest...) }

// SubscriptionsExpiredBetween — мерзімі (from, to] аралығында біткен, орнына басқа ақылы тариф
// алынбаған және kind түріндегі хабарламасы әлі жасалмаған жазылымдар.
//
// A subscription replaced by a purchase or an admin change is "cancelled",
// not expired, so it never produces this notice.
func (s *Store) SubscriptionsExpiredBetween(ctx context.Context, from, to time.Time, kind string) ([]SubscriptionNotice, error) {
	return s.subscriptionNotices(ctx, `
		SELECT s.id, s.user_id, s.expires_at, p.name_kk, p.name_ru, p.name_en, p.name_uz
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id JOIN users u ON u.id = s.user_id
		WHERE s.expires_at > ? AND s.expires_at <= ? AND s.status IN ('active','trial','expired') AND p.is_free = 0
		  AND u.status = 'active' AND u.kind = 'account' AND u.deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM subscriptions s2 JOIN plans p2 ON p2.id = s2.plan_id
		                  WHERE s2.user_id = s.user_id AND s2.id <> s.id AND p2.is_free = 0
		                    AND s2.status IN ('active','trial') AND (s2.expires_at IS NULL OR s2.expires_at > ?))
		  AND NOT `+noticeSent+`
		ORDER BY s.expires_at LIMIT ?`, ms(from), ms(to), ms(to), kind, maxSubscriptionNotices)
}
