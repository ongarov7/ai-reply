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
// installations, users and subscriptions. Values always travel as bind
// arguments; the only text concatenated into the query is this file's own
// fixed fragments, chosen by validated enum values — never a field name or an
// expression from a request.
type AudienceQuery struct {
	Filter   domain.AudienceFilter
	Category string
	Now      time.Time
	// Platforms — platforms whose provider is configured; others are skipped.
	Platforms []string
	// StaleAfter — installations not seen for this long are not reachable
	// (FCM drops tokens unused for 270 days).
	StaleAfter time.Duration
	// UserID — only this account's installations (per-user notifications).
	UserID string
	// Location — the timezone registration dates are given in.
	Location *time.Location
}

// paidNow — қолданушының қазір белсенді ақылы тарифі бар.
const paidNow = `EXISTS (SELECT 1 FROM subscriptions s JOIN plans p ON p.id = s.plan_id
	WHERE s.user_id = i.user_id AND p.is_free = 0 AND s.status IN ('active','trial')
	AND (s.expires_at IS NULL OR s.expires_at > ?))`

// paidEver — қолданушыда бір кезде ақылы тариф болған.
const paidEver = `EXISTS (SELECT 1 FROM subscriptions s JOIN plans p ON p.id = s.plan_id
	WHERE s.user_id = i.user_id AND p.is_free = 0)`

// matchClause — сүзгі шарттары (қол жетімділікке қарамай).
func matchClause(q AudienceQuery) (string, []any) {
	f := q.Filter
	now := ms(q.Now)
	where := []string{"(i.user_id IS NULL OR (u.status = 'active' AND u.deleted_at IS NULL))"}
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
	switch f.Auth {
	case domain.AuthAuthenticated:
		where = append(where, "i.user_id IS NOT NULL")
	case domain.AuthAnonymous:
		where = append(where, "i.user_id IS NULL")
	}
	if f.NeedsAccount() {
		where = append(where, "i.user_id IS NOT NULL")
	}
	switch f.Payment {
	case "paid":
		where = append(where, paidNow)
		args = append(args, now)
	case "unpaid":
		where = append(where, "NOT "+paidNow)
		args = append(args, now)
	}
	switch f.Subscription {
	case "active":
		where = append(where, paidNow)
		args = append(args, now)
	case "expired":
		where = append(where, "NOT "+paidNow, paidEver)
		args = append(args, now)
	case "none":
		where = append(where, "NOT "+paidEver)
	}
	if len(f.Locales) > 0 {
		where = append(where, "i.locale IN ("+placeholders(len(f.Locales))+")")
		for _, l := range f.Locales {
			args = append(args, l)
		}
	}
	if f.AppVersionMin != "" {
		where = append(where, "i.app_version_num >= ?")
		args = append(args, domain.VersionNumber(f.AppVersionMin))
	}
	if f.AppVersionMax != "" {
		where = append(where, "i.app_version_num > 0 AND i.app_version_num <= ?")
		args = append(args, domain.VersionNumber(f.AppVersionMax))
	}
	if f.OSVersionMin != "" {
		where = append(where, "i.os_version_num >= ?")
		args = append(args, domain.VersionNumber(f.OSVersionMin))
	}
	if f.ActiveWithinDays > 0 {
		where = append(where, "i.last_seen_at >= ?")
		args = append(args, ms(q.Now.AddDate(0, 0, -f.ActiveWithinDays)))
	}
	if f.InactiveForDays > 0 {
		where = append(where, "i.last_seen_at < ?")
		args = append(args, ms(q.Now.AddDate(0, 0, -f.InactiveForDays)))
	}
	loc := q.Location
	if loc == nil {
		loc = time.UTC
	}
	if f.RegisteredFrom != "" {
		if t, err := time.ParseInLocation("2006-01-02", f.RegisteredFrom, loc); err == nil {
			where = append(where, "u.created_at >= ?")
			args = append(args, ms(t))
		}
	}
	if f.RegisteredTo != "" {
		if t, err := time.ParseInLocation("2006-01-02", f.RegisteredTo, loc); err == nil {
			where = append(where, "u.created_at < ?")
			args = append(args, ms(t.AddDate(0, 0, 1)))
		}
	}
	if len(f.UserIDs) > 0 {
		where = append(where, "i.user_id IN ("+placeholders(len(f.UserIDs))+")")
		for _, id := range f.UserIDs {
			args = append(args, id)
		}
	}
	return strings.Join(where, " AND "), args
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
		where = append(where, `(i.user_id IS NULL OR NOT EXISTS (SELECT 1 FROM notification_preferences np
			WHERE np.user_id = i.user_id AND np.category = ? AND np.enabled = 0))`)
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

const audienceFrom = ` FROM app_installations i LEFT JOIN users u ON u.id = i.user_id WHERE `

// AudiencePreview — алушылар саны (ешнәрсе жіберілмейді).
type AudiencePreview struct {
	MatchedDevices   int `json:"matched_devices"`   // match the filters, reachable or not
	Devices          int `json:"devices"`           // will receive the push
	Users            int `json:"users"`             // distinct accounts among Devices
	AnonymousDevices int `json:"anonymous_devices"` // devices without an account
	Android          int `json:"android"`
	IOS              int `json:"ios"`
}

// PreviewAudience — екі агрегат сұраныс; жолдар жадқа жүктелмейді.
func (s *Store) PreviewAudience(ctx context.Context, q AudienceQuery) (AudiencePreview, error) {
	var out AudiencePreview
	match, matchArgs := matchClause(q)
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*)`+audienceFrom+match, matchArgs...).Scan(&out.MatchedDevices); err != nil {
		return out, err
	}
	where, args := eligible(q)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT i.user_id),
		       COALESCE(SUM(CASE WHEN i.user_id IS NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN i.platform = 'android' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN i.platform = 'ios' THEN 1 ELSE 0 END), 0)`+audienceFrom+where, args...).
		Scan(&out.Devices, &out.Users, &out.AnonymousDevices, &out.Android, &out.IOS)
	return out, err
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
