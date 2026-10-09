package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// emailSendTimeout — хатты жіберуге берілетін ең ұзақ уақыт (қайталаумен бірге).
const emailSendTimeout = 12 * time.Second

// Кодтың жеткізілу тәсілі (otp_codes.channel).
const (
	channelEmail  = "email"
	channelStub   = "stub"   // демо: AUTH_DEMO_OTP, хат жоқ
	channelReview = "review" // App Review / Play review: REVIEW_LOGIN_CODE, хат жоқ
	// channelNone — тіркелгісі жоқ поштаға жою коды: хат жоқ, кодты ешкім алмайды.
	channelNone = "none"
)

// EmailChallenge — кодты сұрау нәтижесі.
//
// It is the same for an address we know and one we have never seen, so the
// endpoint cannot be used to find out who has an account.
type EmailChallenge struct {
	MaskedEmail string
	ExpiresIn   int // секунд
	ResendAfter int // секунд
	CodeLength  int
	DemoMode    bool
}

// RequestEmailOTP — поштаға кіру коды. Тіркелу мен кіру — бір ағын.
func (s *Service) RequestEmailOTP(ctx context.Context, rawEmail, locale string) (EmailChallenge, error) {
	return s.requestEmailOTP(ctx, rawEmail, locale, domain.OTPPurposeLogin, false)
}

// VerifyEmailOTP — кодты тексереді, тіркелгіні табады не ашады және сессия береді.
func (s *Service) VerifyEmailOTP(ctx context.Context, rawEmail, code string, info DeviceInfo) (Session, error) {
	address, err := s.consumeEmailOTP(ctx, rawEmail, code, domain.OTPPurposeLogin)
	if err != nil {
		return Session{}, err
	}
	user, isNew, err := s.userForVerifiedEmail(ctx, address, info)
	if err != nil {
		return Session{}, err
	}
	return s.openSession(ctx, user, isNew, info)
}

// RequestLinkEmailOTP — поштасы жоқ тіркелгіге (мысалы, телефонмен ашылған) пошта қосу коды.
//
// This is how accounts opened with a phone number keep a way back in once
// the apps stop offering phone sign-in: the user proves an address once while
// still signed in, and from then on signs in with it.
func (s *Service) RequestLinkEmailOTP(ctx context.Context, user domain.User, rawEmail, locale string) (EmailChallenge, error) {
	if user.Email != "" {
		return EmailChallenge{}, domain.ErrConflict
	}
	return s.requestEmailOTP(ctx, rawEmail, locale, domain.OTPPurposeLinkEmail, false)
}

// VerifyLinkEmailOTP — кодты тексеріп, поштаны осы тіркелгіге бекітеді.
func (s *Service) VerifyLinkEmailOTP(ctx context.Context, user domain.User, rawEmail, code string) (domain.User, error) {
	if user.Email != "" {
		return domain.User{}, domain.ErrConflict
	}
	address, err := s.consumeEmailOTP(ctx, rawEmail, code, domain.OTPPurposeLinkEmail)
	if err != nil {
		return domain.User{}, err
	}
	if err := s.repo.AttachEmail(ctx, user.ID, address, s.clock.Now()); err != nil {
		return domain.User{}, err
	}
	s.log.Info("email linked to account")
	return s.repo.UserByID(ctx, user.ID)
}

// requestEmailOTP — кодты шығарады. silent: хатсыз және ешкім білмейтін код.
//
// The silent mode serves account deletion for an address without an account:
// it runs the same cooldown, quotas, failure lock-out and attempt limit and
// stores a random four-digit code like any other, only never sent, so neither
// the confirm answers nor the sign-in cooldown tell whether the address has an
// account. A lucky guess reaches an account that does not exist and gets the
// same {"deleted": true} a real deletion gets.
func (s *Service) requestEmailOTP(ctx context.Context, rawEmail, locale, purpose string, silent bool) (EmailChallenge, error) {
	address, err := NormalizeEmail(rawEmail)
	if err != nil {
		return EmailChallenge{}, err
	}
	now := s.clock.Now()
	cooldown := s.cfg.OTPResendCooldown

	// The resend wait is the server's rule; the app's countdown only mirrors it.
	latest, err := s.repo.LatestOTP(ctx, domain.IdentityEmail, address)
	switch {
	case err == nil && latest.ConsumedReason != domain.OTPReasonDeliveryFailed:
		if wait := latest.CreatedAt.Add(cooldown).Sub(now); wait > 0 {
			return EmailChallenge{}, domain.RetryAfter(domain.ErrOTPCooldown, wait)
		}
	case err != nil && !errors.Is(err, domain.ErrNotFound):
		return EmailChallenge{}, err
	}
	if err := s.checkEmailQuota(ctx, address, now); err != nil {
		return EmailChallenge{}, err
	}
	// An address locked by wrong guesses gets no new code until the window passes.
	if err := s.checkEmailFailures(ctx, address, now); err != nil {
		return EmailChallenge{}, err
	}

	code, channel, err := s.emailCode(address, silent)
	if err != nil {
		return EmailChallenge{}, err
	}
	deliver := channel == channelEmail
	record, err := s.repo.IssueOTP(ctx, repository.OTPRecord{
		Kind:        domain.IdentityEmail,
		Value:       address,
		Purpose:     purpose,
		Channel:     channel,
		CodeHash:    hashEmailOTP(s.otpKey, purpose, address, code),
		MaxAttempts: s.cfg.OTPMaxAttempts,
		ExpiresAt:   now.Add(s.cfg.OTPTTL),
	}, now, now.Add(-cooldown))
	if errors.Is(err, domain.ErrOTPCooldown) {
		// A parallel request for the same address issued a code first.
		return EmailChallenge{}, domain.RetryAfter(domain.ErrOTPCooldown, cooldown)
	}
	if err != nil {
		return EmailChallenge{}, err
	}

	if deliver {
		sendCtx, cancel := context.WithTimeout(ctx, emailSendTimeout)
		err := s.mail.SendOTP(sendCtx, email.OTPMessage{
			To: address, Code: code, Locale: locale, TTL: s.cfg.OTPTTL, Reference: record.ID, Purpose: purpose,
		})
		cancel()
		if err != nil {
			// The code never reached the user: close it, so it can neither be
			// used nor hold back an immediate retry.
			_ = s.repo.CloseOTP(context.WithoutCancel(ctx), record.ID, domain.OTPReasonDeliveryFailed, now)
			s.log.Warn("email otp delivery failed", "purpose", purpose, "error", err.Error())
			return EmailChallenge{}, domain.ErrEmailDelivery
		}
	}

	if channel == channelReview {
		// The address stays out of the log, as for every other code.
		s.log.Info("review login code issued", "purpose", purpose)
	} else {
		s.log.Info("email otp issued", "purpose", purpose, "channel", channel)
	}
	return EmailChallenge{
		MaskedEmail: maskEmail(address),
		ExpiresIn:   int(s.cfg.OTPTTL / time.Second),
		ResendAfter: int((cooldown + time.Second - 1) / time.Second),
		CodeLength:  otpDigits,
		DemoMode:    channel == channelStub,
	}, nil
}

// checkEmailQuota — бір поштаға сағаттық және тәуліктік шек.
//
// Counted from the codes table, so the limits survive a restart and hold no
// matter how many IPs the requests come from. With four digits and five
// attempts per code, these limits are what keep guessing impractical.
func (s *Service) checkEmailQuota(ctx context.Context, address string, now time.Time) error {
	issued, err := s.repo.OTPIssuedSince(ctx, domain.IdentityEmail, address, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if limit := s.cfg.OTPRequestsPerDay; limit > 0 && len(issued) >= limit {
		oldest := issued[len(issued)-limit]
		return domain.RetryAfter(domain.ErrRateLimited, oldest.Add(24*time.Hour).Sub(now))
	}
	hourAgo := now.Add(-time.Hour)
	var lastHour []time.Time
	for _, at := range issued {
		if at.After(hourAgo) {
			lastHour = append(lastHour, at)
		}
	}
	if limit := s.cfg.OTPRequestsPerHour; limit > 0 && len(lastHour) >= limit {
		oldest := lastHour[len(lastHour)-limit]
		return domain.RetryAfter(domain.ErrRateLimited, oldest.Add(time.Hour).Sub(now))
	}
	return nil
}

// checkEmailFailures — бір поштаға тәулігіне қате код енгізу шегі (OTP_MAX_FAILED_PER_DAY).
//
// The attempt limit stops guessing one code; this stops guessing across many:
// four digits and a fresh code every half a minute would otherwise add up.
// Counted from the codes table, like the issue caps, so it holds across IPs
// and restarts. The wait lasts until enough of the oldest failures leave the
// 24-hour window.
func (s *Service) checkEmailFailures(ctx context.Context, address string, now time.Time) error {
	limit := s.cfg.OTPMaxFailedPerDay
	if limit <= 0 {
		return nil
	}
	failures, err := s.repo.OTPFailuresSince(ctx, domain.IdentityEmail, address, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	total := 0
	for _, f := range failures {
		total += f.Failed
	}
	for _, f := range failures {
		if total < limit {
			break
		}
		total -= f.Failed
		if total < limit {
			return domain.RetryAfter(domain.ErrRateLimited, f.CreatedAt.Add(24*time.Hour).Sub(now))
		}
	}
	return nil
}

// emailCode — жаңа код және оның жеткізілу тәсілі (silent — жоғарыдағы requestEmailOTP-ты қараңыз).
//
// The review address gets the configured review code without a mail, so App
// Review and Play review can sign in from the submission notes; every cap
// and attempt limit still applies to it. Without a mail provider only demo
// mode issues codes: the fixed demo code.
func (s *Service) emailCode(address string, silent bool) (code, channel string, err error) {
	if silent {
		// Fails exactly when a real code would: no provider and no demo mode.
		if s.mail == nil && !s.cfg.DemoMode && !(s.cfg.ReviewLogin() && address == s.cfg.ReviewEmail) {
			return "", "", domain.ErrEmailDelivery
		}
		code, err := NewOTPCode(s.random)
		return code, channelNone, err
	}
	if s.cfg.ReviewLogin() && address == s.cfg.ReviewEmail {
		return s.cfg.ReviewCode, channelReview, nil
	}
	if s.mail != nil {
		code, err := NewOTPCode(s.random)
		return code, channelEmail, err
	}
	if s.cfg.DemoMode {
		return s.cfg.DemoOTP, channelStub, nil
	}
	return "", "", domain.ErrEmailDelivery
}

// consumeEmailOTP — кодты тексеріп жабады; сәтті болса нормаланған поштаны қайтарады.
//
// Every guess is counted before it is compared, in one atomic statement, so
// parallel requests cannot exceed the attempt limit, and the final close is
// conditional, so one code opens at most one session.
func (s *Service) consumeEmailOTP(ctx context.Context, rawEmail, code, purpose string) (string, error) {
	address, err := NormalizeEmail(rawEmail)
	if err != nil {
		return "", err
	}
	code = strings.TrimSpace(code)
	if !isOTPCode(code) {
		return "", domain.ErrInvalidOTP
	}

	record, err := s.repo.LatestOTP(ctx, domain.IdentityEmail, address)
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.ErrInvalidOTP
	}
	if err != nil {
		return "", err
	}
	if record.Purpose != purpose {
		return "", domain.ErrInvalidOTP
	}
	if record.ConsumedAt != nil {
		return "", closedOTPError(record)
	}
	now := s.clock.Now()
	if !now.Before(record.ExpiresAt) {
		_ = s.repo.CloseOTP(ctx, record.ID, domain.OTPReasonExpired, now)
		return "", domain.ErrOTPExpired
	}
	if err := s.checkEmailFailures(ctx, address, now); err != nil {
		return "", err
	}

	attempts, err := s.repo.RegisterOTPAttempt(ctx, record.ID, now)
	if errors.Is(err, domain.ErrNotFound) {
		// Closed or used up by a parallel request since it was read.
		if fresh, lookupErr := s.repo.OTPByID(ctx, record.ID); lookupErr == nil && fresh.ConsumedAt != nil {
			return "", closedOTPError(fresh)
		}
		return "", domain.ErrOTPAttemptsExceeded
	}
	if err != nil {
		return "", err
	}

	if !CompareCode(record.CodeHash, hashEmailOTP(s.otpKey, purpose, address, code)) {
		remaining := record.MaxAttempts - attempts
		if remaining <= 0 {
			_ = s.repo.CloseOTP(ctx, record.ID, domain.OTPReasonAttempts, now)
			return "", domain.ErrOTPAttemptsExceeded
		}
		return "", &domain.OTPAttemptError{Remaining: remaining}
	}
	if err := s.repo.CloseOTP(ctx, record.ID, domain.OTPReasonVerified, now); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", domain.ErrOTPAlreadyUsed
		}
		return "", err
	}
	return address, nil
}

// closedOTPError — жабылған кодқа сәйкес қате.
func closedOTPError(record repository.OTPRecord) error {
	switch record.ConsumedReason {
	case domain.OTPReasonVerified:
		return domain.ErrOTPAlreadyUsed
	case domain.OTPReasonAttempts:
		return domain.ErrOTPAttemptsExceeded
	case domain.OTPReasonExpired:
		return domain.ErrOTPExpired
	default:
		// Replaced by a newer code, or never delivered: nothing valid to try.
		return domain.ErrInvalidOTP
	}
}

// accountHoldingEmail — поштаны иеленген тіркелгі: алдымен кіру тәсілі, содан кейін users.email.
//
// users.email alone covers accounts from before e-mail identities existed and
// accounts opened by a provider that was not authoritative for the address:
// whoever proves the mailbox reaches them, exactly as with e-mail recovery.
func (s *Service) accountHoldingEmail(ctx context.Context, address string) (domain.User, error) {
	user, err := s.repo.UserByAuthIdentity(ctx, domain.IdentityEmail, address)
	if errors.Is(err, domain.ErrNotFound) {
		user, err = s.repo.UserByIdentity(ctx, domain.IdentityEmail, address)
	}
	return user, err
}

// userForVerifiedEmail — дәлелденген пошта бойынша тіркелгі: бар болса сол, жоқ болса жаңасы.
func (s *Service) userForVerifiedEmail(ctx context.Context, address string, info DeviceInfo) (domain.User, bool, error) {
	now := s.clock.Now()
	user, err := s.accountHoldingEmail(ctx, address)
	switch {
	case err == nil:
		if err := s.repo.ConfirmEmail(ctx, user.ID, address, now); err != nil {
			return domain.User{}, false, err
		}
		s.touchUser(ctx, user, info)
		return user, false, nil
	case !errors.Is(err, domain.ErrNotFound):
		return domain.User{}, false, err
	}

	created, err := s.repo.CreateUserWithIdentities(ctx, s.newUser(info, address), &now, domain.Identity{
		Kind: domain.IdentityEmail, Value: address, ProviderEmail: address,
		ProviderEmailVerified: true, VerifiedAt: &now,
	})
	if errors.Is(err, domain.ErrConflict) {
		// A parallel request created the account a moment ago.
		user, err := s.repo.UserByIdentity(ctx, domain.IdentityEmail, address)
		return user, false, err
	}
	if err != nil {
		return domain.User{}, false, err
	}
	if err := s.provision(ctx, created, info); err != nil {
		return domain.User{}, false, err
	}
	return created, true, nil
}
