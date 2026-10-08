package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const legalConsentColumns = `id, user_id, terms_version, privacy_version, accepted_at,
	locale, platform, app_version, created_at`

func scanLegalConsent(row interface{ Scan(...any) error }) (domain.LegalConsent, error) {
	var consent domain.LegalConsent
	var acceptedAt, createdAt int64
	err := row.Scan(
		&consent.ID,
		&consent.UserID,
		&consent.TermsVersion,
		&consent.PrivacyVersion,
		&acceptedAt,
		&consent.Locale,
		&consent.Platform,
		&consent.AppVersion,
		&createdAt,
	)
	if err != nil {
		return domain.LegalConsent{}, err
	}
	consent.AcceptedAt = timeFrom(acceptedAt)
	consent.CreatedAt = timeFrom(createdAt)
	return consent, nil
}

// SaveLegalConsent stores one acceptance per account and document-version pair.
//
// Accepting the same versions again changes nothing but the client metadata;
// accepting after a withdrawal restores the row with a new accepted_at.
func (s *Store) SaveLegalConsent(ctx context.Context, consent domain.LegalConsent) (domain.LegalConsent, error) {
	if consent.ID == "" {
		consent.ID = traits.NewID()
	}
	now := time.Now().UTC()
	if consent.AcceptedAt.IsZero() {
		consent.AcceptedAt = now
	}
	if consent.CreatedAt.IsZero() {
		consent.CreatedAt = now
	}

	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO legal_consents (
			id, user_id, terms_version, privacy_version, accepted_at,
			locale, platform, app_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, terms_version, privacy_version) DO UPDATE SET
			locale = excluded.locale,
			platform = excluded.platform,
			app_version = excluded.app_version,
			accepted_at = CASE WHEN legal_consents.withdrawn_at IS NULL
			                   THEN legal_consents.accepted_at ELSE excluded.accepted_at END,
			withdrawn_at = NULL`,
		consent.ID,
		consent.UserID,
		consent.TermsVersion,
		consent.PrivacyVersion,
		ms(consent.AcceptedAt),
		consent.Locale,
		consent.Platform,
		consent.AppVersion,
		ms(consent.CreatedAt),
	)
	if err != nil {
		return domain.LegalConsent{}, err
	}
	return s.LegalConsent(ctx, consent.UserID, consent.TermsVersion, consent.PrivacyVersion)
}

// LegalConsent returns acceptance for an exact pair of public document versions.
// A withdrawn acceptance is ErrNotFound.
func (s *Store) LegalConsent(ctx context.Context, userID, termsVersion, privacyVersion string) (domain.LegalConsent, error) {
	row := s.db.Reader().QueryRowContext(ctx, `
		SELECT `+legalConsentColumns+`
		FROM legal_consents
		WHERE user_id = ? AND terms_version = ? AND privacy_version = ? AND withdrawn_at IS NULL`,
		userID, termsVersion, privacyVersion)
	consent, err := scanLegalConsent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LegalConsent{}, domain.ErrNotFound
	}
	return consent, err
}

// WithdrawLegalConsents marks every active acceptance of the account as withdrawn.
// It returns how many rows changed (0 — nothing was active, still not an error).
func (s *Store) WithdrawLegalConsents(ctx context.Context, userID string, at time.Time) (int64, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE legal_consents SET withdrawn_at = ? WHERE user_id = ? AND withdrawn_at IS NULL`,
		ms(at), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
