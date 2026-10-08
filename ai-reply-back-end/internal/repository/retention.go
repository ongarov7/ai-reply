package repository

import (
	"context"
	"time"
)

// Сақтау мерзімі өткен жолдарды кішкене топтармен өшіру (retention қызметі шақырады).
// Each call works in batches through deleteInBatches, so a large backlog never
// holds the single SQLite writer for long.

// DeleteOldOTPCodes — cutoff-тан бұрын шығарылған кіру және жою кодтары.
// The hourly and daily request limits look back one day at most.
func (s *Store) DeleteOldOTPCodes(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM otp_codes WHERE rowid IN (
		SELECT rowid FROM otp_codes WHERE created_at < ? LIMIT ?)`, cutoff, batch)
}

// DeleteDeadRefreshTokens — cutoff-тан бұрын кері қайтарылған не мерзімі біткен refresh токендер.
func (s *Store) DeleteDeadRefreshTokens(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM refresh_tokens WHERE rowid IN (
		SELECT rowid FROM refresh_tokens WHERE MIN(expires_at, COALESCE(revoked_at, expires_at)) < ? LIMIT ?)`,
		cutoff, batch)
}

// DeleteStaleAnonymousInstallations — аккаунтқа байланбаған және cutoff-тан бері көрінбеген орнатулар.
// Their delivery rows go with them (ON DELETE CASCADE).
func (s *Store) DeleteStaleAnonymousInstallations(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM app_installations WHERE rowid IN (
		SELECT rowid FROM app_installations WHERE user_id IS NULL AND last_seen_at < ? LIMIT ?)`, cutoff, batch)
}

// DeleteOldProductEvents — cutoff-тан ескі өнім оқиғалары.
func (s *Store) DeleteOldProductEvents(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM product_events WHERE rowid IN (
		SELECT rowid FROM product_events WHERE created_at < ? LIMIT ?)`, cutoff, batch)
}

// DeleteOldAIUsageEvents — cutoff-тан ескі сұраныс метадеректері (санағыштар usage_* кестелерінде қалады).
func (s *Store) DeleteOldAIUsageEvents(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM ai_usage_events WHERE rowid IN (
		SELECT rowid FROM ai_usage_events WHERE created_at < ? LIMIT ?)`, cutoff, batch)
}
