package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const WebPaymentHandoffTTL = 60 * time.Second

var ErrWebHandoffInvalid = errors.New("auth: invalid web handoff")

type WebPaymentHandoff struct {
	Code      string
	ExpiresIn int
}

func (WebPaymentHandoff) String() string   { return "web payment handoff (redacted)" }
func (WebPaymentHandoff) GoString() string { return "auth.WebPaymentHandoff{redacted}" }

func webHandoffHash(code string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != code {
		return "", ErrWebHandoffInvalid
	}
	hash := sha256.Sum256([]byte(code))
	return hex.EncodeToString(hash[:]), nil
}

// CreateWebPaymentHandoff binds a one-minute capability to the authenticated
// account. Only its SHA-256 hash is stored, in this environment's database.
func (s *Service) CreateWebPaymentHandoff(ctx context.Context, userID, locale string) (WebPaymentHandoff, error) {
	var random [32]byte
	if _, err := io.ReadFull(s.random, random[:]); err != nil {
		return WebPaymentHandoff{}, errors.New("auth: handoff unavailable")
	}
	code := base64.RawURLEncoding.EncodeToString(random[:])
	hash, _ := webHandoffHash(code)
	err := s.repo.DB().Tx(ctx, func(tx *sql.Tx) error {
		now := s.clock.Now()
		// Bounded maintenance needs no new background worker.
		if _, err := tx.ExecContext(ctx, `DELETE FROM web_auth_handoffs WHERE code_hash IN (SELECT code_hash FROM web_auth_handoffs WHERE expires_at<=? OR consumed_at IS NOT NULL LIMIT 256)`, now.UnixMilli()); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO web_auth_handoffs(code_hash,user_id,locale,created_at,expires_at) SELECT ?,id,?,?,? FROM users WHERE id=? AND status='active' AND deleted_at IS NULL AND kind='account'`, hash, domain.NormalizeLocale(locale), now.UnixMilli(), now.Add(WebPaymentHandoffTTL).UnixMilli(), userID)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWebHandoffInvalid
		}
		return nil
	})
	if err != nil {
		return WebPaymentHandoff{}, err
	}
	return WebPaymentHandoff{Code: code, ExpiresIn: int(WebPaymentHandoffTTL.Seconds())}, nil
}

// PeekWebPaymentHandoff permits the website to ask before switching an existing
// browser account. It neither consumes the capability nor creates a session.
func (s *Service) PeekWebPaymentHandoff(ctx context.Context, code string) (domain.User, error) {
	hash, err := webHandoffHash(code)
	if err != nil {
		return domain.User{}, err
	}
	var userID string
	err = s.repo.DB().Reader().QueryRowContext(ctx, `SELECT h.user_id FROM web_auth_handoffs h JOIN users u ON u.id=h.user_id WHERE h.code_hash=? AND h.consumed_at IS NULL AND h.expires_at>? AND u.status='active' AND u.deleted_at IS NULL AND u.kind='account'`, hash, s.clock.Now().UnixMilli()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrWebHandoffInvalid
	}
	if err != nil {
		return domain.User{}, err
	}
	user, err := s.repo.UserByID(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) || err == nil && (user.Status != domain.UserActive || user.Kind != "account") {
		return domain.User{}, ErrWebHandoffInvalid
	}
	return user, err
}

// ConsumeWebPaymentHandoff claims the code atomically, then issues an independent
// web device/session. It never rotates or reuses a native device/token family.
func (s *Service) ConsumeWebPaymentHandoff(ctx context.Context, code string, info DeviceInfo) (Session, error) {
	hash, err := webHandoffHash(code)
	if err != nil {
		return Session{}, err
	}
	var userID, locale string
	err = s.repo.DB().Tx(ctx, func(tx *sql.Tx) error {
		now := s.clock.Now().UnixMilli()
		err := tx.QueryRowContext(ctx, `SELECT h.user_id,h.locale FROM web_auth_handoffs h JOIN users u ON u.id=h.user_id WHERE h.code_hash=? AND h.consumed_at IS NULL AND h.expires_at>? AND u.status='active' AND u.deleted_at IS NULL AND u.kind='account'`, hash, now).Scan(&userID, &locale)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWebHandoffInvalid
		}
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE web_auth_handoffs SET consumed_at=? WHERE code_hash=? AND consumed_at IS NULL AND expires_at>?`, now, hash, now)
		if err != nil {
			return err
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrWebHandoffInvalid
		}
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	user, err := s.repo.UserByID(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) || err == nil && (user.Status != domain.UserActive || user.Kind != "account") {
		return Session{}, ErrWebHandoffInvalid
	}
	if err != nil {
		return Session{}, err
	}
	info.Platform, info.DeviceID = domain.PlatformWeb, traits.NewID()
	info.Locale = locale
	return s.openSession(ctx, user, false, info)
}
