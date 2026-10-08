package auth

import (
	"context"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// deletionRequestTimeout — фонда жою кодын шығаруға және хатты жіберуге берілетін уақыт.
const deletionRequestTimeout = emailSendTimeout + 5*time.Second

// RequestAccountDeletion — /account/delete бетінен: тіркелгі бар болса, поштаға жою коды кетеді.
//
// The answer never says whether an account exists: an unknown address, the
// resend cooldown, the per-address limits and a failed delivery all look the
// same to the caller. The code is issued in the background, so the response
// time does not tell either. An unknown address gets a silent code (a random
// code that is never sent) through the same checks, so the confirm step and
// the sign-in cooldown behave the same for it too. Only a malformed address
// is refused, and that says nothing about accounts.
func (s *Service) RequestAccountDeletion(ctx context.Context, rawEmail, locale string) error {
	address, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), deletionRequestTimeout)
		defer cancel()
		s.issueDeletionCode(bg, address, locale)
	}()
	return nil
}

func (s *Service) issueDeletionCode(ctx context.Context, address, locale string) {
	silent := false
	if _, err := s.accountHoldingEmail(ctx, address); err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			s.log.Error("account deletion code lookup failed", "error", err.Error())
			return
		}
		silent = true // тіркелгі жоқ: хатсыз код, тексерулер бірдей
	}
	if _, err := s.requestEmailOTP(ctx, address, locale, domain.OTPPurposeDelete, silent); err != nil {
		// Cooldown, limits or delivery: the person can ask again later.
		s.log.Info("account deletion code not issued", "reason", err.Error())
	}
}

// ConfirmAccountDeletion — жою кодын тексеріп, жойылатын тіркелгіні қайтарады.
//
// A wrong, expired or used code fails exactly like sign-in (INVALID_OTP,
// OTP_EXPIRED, …). An address without an account holds a silent code nobody
// received, so it fails the same way, attempts_remaining and lock-out
// included; a lucky guess finds no account (ErrNotFound), which the handler
// answers like a deletion. A sign-in code is never accepted here, nor a
// deletion code for sign-in: the purpose is part of the code's hash.
func (s *Service) ConfirmAccountDeletion(ctx context.Context, rawEmail, code string) (domain.User, error) {
	address, err := s.consumeEmailOTP(ctx, rawEmail, code, domain.OTPPurposeDelete)
	if err != nil {
		return domain.User{}, err
	}
	return s.accountHoldingEmail(ctx, address)
}

// Wait — фондағы жою кодтарының аяқталуын күтеді (сервер тоқтағанда және тестте).
func (s *Service) Wait() { s.background.Wait() }
