package repository

import (
	"context"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// AudienceQuery — push алушыларын таңдау шарттары.
//
// Everything a campaign can target is expressed here as SQL over
// installations, users, subscriptions, plans and today's usage. Values always
// travel as bind arguments; the only text concatenated into the query is this
// file's own fixed fragments, chosen by validated enum values — never a field
// name or an expression from a request.
type AudienceQuery struct {
	Filter domain.AudienceFilter
	// Recipients — Filter.UserIDs plus the accounts Filter.Emails resolved to.
	// Used only when the filter names people: an empty list then matches nobody.
	Recipients []string
	Category   string
	Now        time.Time
	// Today — the usage_daily date (YYYY-MM-DD) in the app timezone.
	Today string
	// DefaultPlanID — the plan of an account without a usable subscription.
	DefaultPlanID string
	// QuotaLowPercent — the "near exhaustion" share of the daily limit.
	QuotaLowPercent int
	// Platforms — platforms push can reach now; with none nothing is reachable.
	Platforms []string
	// StaleAfter — installations not seen for this long are not reachable
	// (FCM drops tokens unused for 270 days).
	StaleAfter time.Duration
	// UserID — only this account's installations (per-user notifications).
	UserID string
	// Language — only installations whose notification language is this one (campaign fan-out).
	Language string
}

// supportedLanguages — SQL тізімі: 'kk','ru','en','uz' (domain.Locales тұрақты мәндерінен).
var supportedLanguages = func() string {
	quoted := make([]string, len(domain.Locales))
	for i, l := range domain.Locales {
		quoted[i] = "'" + l + "'"
	}
	return strings.Join(quoted, ",")
}()

// resolvedLanguage — бір орнатуға жіберілетін хабарламаның тілі: қолданушы таңдаған
// тіл → құрылғы тілі → тіркелгі тілі → en.
var resolvedLanguage = `(CASE
	WHEN u.preferred_language IN (` + supportedLanguages + `) THEN u.preferred_language
	WHEN i.locale IN (` + supportedLanguages + `) THEN i.locale
	WHEN u.locale IN (` + supportedLanguages + `) THEN u.locale
	ELSE 'en' END)`

// usableSubscription — жазылым қазір жарамды (subscriptions s, бір аргумент: now).
const usableSubscription = `s.status IN ('active','trial') AND (s.expires_at IS NULL OR s.expires_at > ?)`

// demoSubscription — тариф сынақ мерзімімен не demo провайдерінің төлемімен алынған.
const demoSubscription = `(s.status = 'trial' OR (s.source = 'payment' AND (
	SELECT pay.provider FROM payments pay
	WHERE pay.user_id = s.user_id AND pay.plan_id = s.plan_id AND pay.status = 'succeeded'
	ORDER BY pay.updated_at DESC LIMIT 1) = 'demo'))`

// paidSubscription — ақылы тарифтің жарамды жазылымы бар (бір аргумент: now).
func paidSubscription(extra string) string {
	return `EXISTS (SELECT 1 FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		WHERE s.user_id = u.id AND p.is_free = 0 AND ` + usableSubscription + extra + `)`
}

// paidEver — ақылы тариф бір кезде болған.
const paidEver = `EXISTS (SELECT 1 FROM subscriptions s JOIN plans p ON p.id = s.plan_id
	WHERE s.user_id = u.id AND p.is_free = 0 AND s.status IN ('active','trial','expired','cancelled'))`

// currentPlan — тіркелгінің қазіргі тарифі: жарамды жазылымы, болмаса әдепкі тегін тариф
// (екі аргумент: now, default plan id).
const currentPlan = `COALESCE((SELECT s.plan_id FROM subscriptions s
	WHERE s.user_id = u.id AND ` + usableSubscription + `
	ORDER BY CASE s.status WHEN 'active' THEN 0 ELSE 1 END, s.created_at DESC LIMIT 1), ?)`

// matchClause — сүзгі шарттары (қол жетімділікке қарамай).
//
// Only installations attached to an active, ordinary account: anonymous
// devices, the simulator account and legacy install tokens are never part of
// an audience.
func matchClause(q AudienceQuery) (string, []any) {
	f := q.Filter
	now := ms(q.Now)
	where := []string{"u.status = 'active'", "u.deleted_at IS NULL", "u.kind = 'account'"}
	args := []any{}

	if q.UserID != "" {
		where = append(where, "i.user_id = ?")
		args = append(args, q.UserID)
	}
	if len(f.Platforms) > 0 {
		where = append(where, "i.platform IN ("+placeholders(len(f.Platforms))+")")
		for _, p := range f.Platforms {
			args = append(args, p)
		}
	}
	switch f.Segment {
	case domain.SegmentFree:
		where = append(where, "NOT "+paidSubscription(""))
		args = append(args, now)
	case domain.SegmentPaid:
		where = append(where, paidSubscription(" AND NOT "+demoSubscription))
		args = append(args, now)
	case domain.SegmentDemo:
		where = append(where, paidSubscription(" AND "+demoSubscription))
		args = append(args, now)
	}
	switch f.Subscription {
	case domain.SubscriptionFilterActive:
		where = append(where, paidSubscription(""))
		args = append(args, now)
	case domain.SubscriptionFilterExpired:
		where = append(where, paidEver, "NOT "+paidSubscription(""))
		args = append(args, now)
	}
	if len(f.PlanIDs) > 0 {
		where = append(where, currentPlan+" IN ("+placeholders(len(f.PlanIDs))+")")
		args = append(args, now, q.DefaultPlanID)
		for _, id := range f.PlanIDs {
			args = append(args, id)
		}
	}
	if len(f.Languages) > 0 {
		where = append(where, resolvedLanguage+" IN ("+placeholders(len(f.Languages))+")")
		for _, l := range f.Languages {
			args = append(args, l)
		}
	}
	if q.Language != "" {
		where = append(where, resolvedLanguage+" = ?")
		args = append(args, q.Language)
	}
	if f.Quota != "" {
		clause, quotaArgs := quotaClause(q)
		where = append(where, clause)
		args = append(args, quotaArgs...)
	}
	if f.Specific() {
		if len(q.Recipients) == 0 {
			where = append(where, "0 = 1")
		} else {
			where = append(where, "i.user_id IN ("+placeholders(len(q.Recipients))+")")
			for _, id := range q.Recipients {
				args = append(args, id)
			}
		}
	}
	return strings.Join(where, " AND "), args
}

// quotaClause — бүгінгі қалдық: has_remaining (шектен көп), near_exhaustion
// (0 < қалдық ≤ шек), exhausted (қалдық жоқ). Шек = max(1, ceil(лимит × % / 100)).
func quotaClause(q AudienceQuery) (string, []any) {
	limit := `COALESCE((SELECT p.daily_message_limit FROM plans p WHERE p.id = ` + currentPlan + `), 0)`
	limitArgs := []any{ms(q.Now), q.DefaultPlanID}
	used := `COALESCE((SELECT ud.used FROM usage_daily ud WHERE ud.user_id = u.id AND ud.usage_date = ?), 0)`
	remaining := "(" + limit + " - " + used + ")"
	remainingArgs := append(append([]any{}, limitArgs...), q.Today)
	threshold := "MAX(1, (" + limit + " * ? + 99) / 100)"
	thresholdArgs := append(append([]any{}, limitArgs...), q.QuotaLowPercent)

	switch q.Filter.Quota {
	case domain.QuotaExhausted:
		return remaining + " <= 0", remainingArgs
	case domain.QuotaNearExhausted:
		args := append(append(append([]any{}, remainingArgs...), remainingArgs...), thresholdArgs...)
		return remaining + " > 0 AND " + remaining + " <= " + threshold, args
	default: // domain.QuotaHasRemaining
		return remaining + " > " + threshold, append(append([]any{}, remainingArgs...), thresholdArgs...)
	}
}

// reachableClause — хабарламаны шынымен жеткізуге болатын орнатулар.
//
// A live token, the in-app switch on, OS permission that shows alerts, a
// configured provider, seen recently enough, and — unless the category is
// security — the account has not switched this category off.
func reachableClause(q AudienceQuery) (string, []any) {
	where := []string{
		"i.push_status = 'active'",
		"i.push_token_sealed IS NOT NULL",
		"i.notifications_enabled = 1",
		"i.push_permission IN ('authorized','provisional','ephemeral','unknown')",
	}
	var args []any
	if len(q.Platforms) == 0 {
		where = append(where, "0 = 1")
	} else {
		where = append(where, "i.platform IN ("+placeholders(len(q.Platforms))+")")
		for _, p := range q.Platforms {
			args = append(args, p)
		}
	}
	if q.StaleAfter > 0 {
		where = append(where, "i.last_seen_at >= ?")
		args = append(args, ms(q.Now.Add(-q.StaleAfter)))
	}
	if domain.CategoryOptional(q.Category) {
		where = append(where, `NOT EXISTS (SELECT 1 FROM notification_preferences np
			WHERE np.user_id = i.user_id AND np.category = ? AND np.enabled = 0)`)
		args = append(args, q.Category)
	}
	return strings.Join(where, " AND "), args
}

// eligible — сүзгі + қол жетімділік: жаппай жіберу мен алдын ала санау бір шартты қолданады.
func eligible(q AudienceQuery) (string, []any) {
	match, matchArgs := matchClause(q)
	reach, reachArgs := reachableClause(q)
	return match + " AND " + reach, append(matchArgs, reachArgs...)
}

const audienceFrom = ` FROM app_installations i JOIN users u ON u.id = i.user_id WHERE `

// LanguageReach — бір тілде хабарлама алатындар.
type LanguageReach struct {
	Users   int `json:"users"`
	Devices int `json:"devices"`
}

// AudiencePreview — алушылар саны (ешнәрсе жіберілмейді).
type AudiencePreview struct {
	MatchedDevices int                      `json:"matched_devices"` // match the filters, reachable or not
	Devices        int                      `json:"devices"`         // will receive the push
	Users          int                      `json:"users"`           // distinct accounts among Devices
	Android        int                      `json:"android"`
	IOS            int                      `json:"ios"`
	ByLanguage     map[string]LanguageReach `json:"by_language"`
	// Unresolved — e-mails from the filter that belong to no account.
	Unresolved []string `json:"unresolved"`
}

// PreviewAudience — агрегат сұраныстар; жолдар жадқа жүктелмейді.
func (s *Store) PreviewAudience(ctx context.Context, q AudienceQuery) (AudiencePreview, error) {
	out := AudiencePreview{ByLanguage: map[string]LanguageReach{}, Unresolved: []string{}}
	for _, l := range domain.Locales {
		out.ByLanguage[l] = LanguageReach{}
	}
	match, matchArgs := matchClause(q)
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*)`+audienceFrom+match, matchArgs...).Scan(&out.MatchedDevices); err != nil {
		return out, err
	}
	where, args := eligible(q)
	if err := s.db.Reader().QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT i.user_id),
		       COALESCE(SUM(CASE WHEN i.platform = 'android' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN i.platform = 'ios' THEN 1 ELSE 0 END), 0)`+audienceFrom+where, args...).
		Scan(&out.Devices, &out.Users, &out.Android, &out.IOS); err != nil {
		return out, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT `+resolvedLanguage+` AS language, COUNT(*), COUNT(DISTINCT i.user_id)`+audienceFrom+where+`
		GROUP BY language`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var language string
		var reach LanguageReach
		if err := rows.Scan(&language, &reach.Devices, &reach.Users); err != nil {
			return out, err
		}
		out.ByLanguage[language] = reach
	}
	return out, rows.Err()
}

// ResolveAccountEmails — поштадан тіркелгіге (тек кәдімгі тіркелгілер). Табылмағаны бөлек.
func (s *Store) ResolveAccountEmails(ctx context.Context, emails []string) (ids, unresolved []string, err error) {
	if len(emails) == 0 {
		return nil, nil, nil
	}
	args := make([]any, len(emails))
	for i, e := range emails {
		args[i] = e
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, email FROM users
		WHERE email IN (`+placeholders(len(emails))+`) AND deleted_at IS NULL AND kind = 'account'`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, nil, err
		}
		found[email] = true
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, e := range emails {
		if !found[e] {
			unresolved = append(unresolved, e)
		}
	}
	return ids, unresolved, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
