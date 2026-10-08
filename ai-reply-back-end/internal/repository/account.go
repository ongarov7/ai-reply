package repository

import (
	"context"
	"database/sql"
	"errors"
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

		res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
		return affected(res, err)
	})
}
