package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// DeleteUserAccount — тіркелгіні және оның барлық серверлік деректерін бір транзакцияда жояды.
//
// One write transaction (BEGIN IMMEDIATE on the single writer), all or nothing:
//   - otp_codes has no user column, so codes are removed by the account's
//     e-mail and phone values (users row and e-mail/phone identities);
//   - queued push deliveries of the account are cancelled, and its app
//     installations are detached with the stored push token cleared, so the
//     phone is no longer addressable as this person (the app registers again
//     anonymously after sign-out);
//   - campaign audience filters that name the account (by id, by its e-mail or
//     by an address a provider gave) lose those entries; they are counted in
//     redacted_people, so the filter stays specific and never widens to everyone;
//   - DELETE FROM users removes everything else through ON DELETE CASCADE
//     (foreign_keys=on): profile, identities, devices, refresh tokens,
//     subscriptions, usage counters and events, payments, legal consents,
//     product events, notification preferences and notifications, AI reports.
//
// ErrNotFound — no such account (already deleted).
func (s *Store) DeleteUserAccount(ctx context.Context, userID string, now time.Time) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var email, phone sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT email, phone FROM users WHERE id = ?`, userID).Scan(&email, &phone)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}

		values := map[string]map[string]bool{domain.IdentityEmail: {}, domain.IdentityPhone: {}}
		if email.String != "" {
			values[domain.IdentityEmail][email.String] = true
		}
		if phone.String != "" {
			values[domain.IdentityPhone][phone.String] = true
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT kind, value FROM auth_identities WHERE user_id = ? AND kind IN (?, ?)`,
			userID, domain.IdentityEmail, domain.IdentityPhone)
		if err != nil {
			return err
		}
		for rows.Next() {
			var kind, value string
			if err := rows.Scan(&kind, &value); err != nil {
				rows.Close()
				return err
			}
			values[kind][value] = true
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for kind, set := range values {
			if len(set) == 0 {
				continue
			}
			args := []any{kind}
			for value := range set {
				args = append(args, value)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM otp_codes WHERE identity_kind = ? AND identity_value IN (`+
				strings.TrimSuffix(strings.Repeat("?,", len(set)), ",")+`)`, args...); err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE notification_deliveries SET status = 'cancelled', error_code = 'account_deleted', updated_at = ?
			WHERE user_id = ? AND status IN ('queued','retrying')`, ms(now), userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE app_installations SET
				user_id = NULL, attached_at = NULL,
				push_token_sealed = NULL, push_token_hash = NULL, token_updated_at = NULL,
				push_status = 'none', push_status_reason = 'account_deleted', push_disabled_at = ?, updated_at = ?
			WHERE user_id = ?`, ms(now), ms(now), userID); err != nil {
			return err
		}

		// A campaign may name the person by any address the account holds,
		// a provider's (Apple relay, Google) included.
		emails := maps.Clone(values[domain.IdentityEmail])
		if err := providerEmails(ctx, tx, userID, emails); err != nil {
			return err
		}
		if err := redactCampaignAudiences(ctx, tx, userID, emails, now); err != nil {
			return err
		}

		res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
		return affected(res, err)
	})
}

// providerEmails — тіркелгінің Google/Apple берген поштасы (кіші әріппен) жиынға қосылады.
func providerEmails(ctx context.Context, tx *sql.Tx, userID string, into map[string]bool) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT provider_email FROM auth_identities WHERE user_id = ? AND provider_email <> ''`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return err
		}
		into[strings.ToLower(strings.TrimSpace(address))] = true
	}
	return rows.Err()
}

// redactCampaignAudiences — науқан сүзгілерінен жойылатын тіркелгінің id-і мен поштасын алады.
//
// The table is small and the JSON shape is domain.AudienceFilter, so rows that
// may name people are decoded and rewritten in Go; LIKE only narrows the scan.
func redactCampaignAudiences(ctx context.Context, tx *sql.Tx, userID string, emails map[string]bool, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, audience_filter FROM notification_campaigns
		WHERE audience_filter LIKE '%"user_ids"%' OR audience_filter LIKE '%"emails"%'`)
	if err != nil {
		return err
	}
	changed := map[string]string{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var f domain.AudienceFilter
		if json.Unmarshal([]byte(raw), &f) != nil || !f.RedactAccount(userID, emails) {
			continue
		}
		encoded, err := json.Marshal(f)
		if err != nil {
			rows.Close()
			return err
		}
		changed[id] = string(encoded)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for id, filter := range changed {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_campaigns SET audience_filter = ?, updated_at = ? WHERE id = ?`,
			filter, ms(now), id); err != nil {
			return err
		}
	}
	return nil
}
