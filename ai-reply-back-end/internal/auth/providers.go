package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth/idtoken"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// IDTokenVerifier — провайдер токенін тексеретін тәуелділік (*idtoken.Verifier).
type IDTokenVerifier interface {
	Verify(ctx context.Context, token, expectedNonce string) (idtoken.Claims, error)
}

// providerSignIn — тексерілген токеннен алынған, кіруге қажет мәліметтер.
type providerSignIn struct {
	kind    string // google | apple
	subject string // провайдердің өзгермейтін `sub` идентификаторы
	email   string // нормаланған; бос болуы мүмкін
	// emailVerified — провайдер поштаны расталған деп белгіледі.
	emailVerified bool
	// emailTrusted — провайдер бұл пошта үшін беделді: осы пошта бар тіркелгіге
	// қосуға және жаңа тіркелгінің поштасы ретінде жазуға болады.
	emailTrusted bool
	displayName  string
}

// SignInWithGoogle — Google ID token арқылы кіру.
//
// The token is checked here against Google's published keys: signature,
// issuer, audience (our own client ids), expiry and the nonce the app made
// for this attempt. Nothing the app says about the user is taken on trust.
func (s *Service) SignInWithGoogle(ctx context.Context, token, nonce string, info DeviceInfo) (Session, error) {
	if s.google == nil {
		return Session{}, domain.ErrAuthProviderUnavailable
	}
	if strings.TrimSpace(nonce) == "" {
		s.tokenRejected(domain.IdentityGoogle, "missing_nonce")
		return Session{}, domain.ErrInvalidIDToken
	}
	claims, err := s.google.Verify(ctx, strings.TrimSpace(token), nonce)
	if err != nil {
		return Session{}, s.providerError(domain.IdentityGoogle, err, nonce)
	}
	address, _ := NormalizeEmail(claims.Email)
	verified := claims.EmailVerified && address != ""
	return s.signInWithProvider(ctx, providerSignIn{
		kind:          domain.IdentityGoogle,
		subject:       claims.Subject,
		email:         address,
		emailVerified: verified,
		emailTrusted:  verified && googleAuthoritative(address, claims.HostedDomain),
		displayName:   cleanDisplayName(claims.Name),
	}, info)
}

// SignInWithApple — Sign in with Apple identity token арқылы кіру.
//
// rawNonce is the value the app kept; the token must carry its SHA-256.
// fullName comes from the app because Apple hands the name over once, on the
// first authorization, and never puts it in the token. It only ever fills an
// empty profile name — it identifies nothing.
func (s *Service) SignInWithApple(ctx context.Context, token, rawNonce, fullName string, info DeviceInfo) (Session, error) {
	if s.apple == nil {
		return Session{}, domain.ErrAuthProviderUnavailable
	}
	if strings.TrimSpace(rawNonce) == "" {
		s.tokenRejected(domain.IdentityApple, "missing_nonce")
		return Session{}, domain.ErrInvalidIDToken
	}
	claims, err := s.apple.Verify(ctx, strings.TrimSpace(token), idtoken.AppleNonce(rawNonce))
	if err != nil {
		return Session{}, s.providerError(domain.IdentityApple, err, rawNonce)
	}
	address, _ := NormalizeEmail(claims.Email)
	verified := claims.EmailVerified && address != ""
	return s.signInWithProvider(ctx, providerSignIn{
		kind:          domain.IdentityApple,
		subject:       claims.Subject,
		email:         address,
		emailVerified: verified,
		emailTrusted:  verified && appleAuthoritative(address, claims.IsPrivateEmail),
		displayName:   cleanDisplayName(fullName),
	}, info)
}

// providerError — тексерілмеген токен: бір ескерту (себеп санатымен) және клиентке қате.
//
// Every rejected token gives exactly one warning, "identity token rejected",
// with the failed check (bad signature or key id, issuer, audience — with the
// aud the token carried and the client ids this server accepts, nonce,
// expiry, …). Those are public identifiers; the token, its subject and the
// e-mail never reach the log. The HTTP answer stays INVALID_ID_TOKEN.
func (s *Service) providerError(kind string, err error, sentNonce string) error {
	if errors.Is(err, idtoken.ErrUnavailable) {
		s.log.Warn("identity provider keys unavailable", "provider", kind, "error", err.Error())
		return domain.ErrAuthProviderUnavailable
	}
	var rejected *idtoken.RejectError
	if !errors.As(err, &rejected) {
		s.tokenRejected(kind, "invalid", "check", err.Error())
		return domain.ErrInvalidIDToken
	}
	attrs := rejected.Attrs()
	if rejected.Reason == idtoken.ReasonNonce {
		attrs = append(attrs, "nonce_form", nonceForm(kind, sentNonce, rejected))
	}
	s.tokenRejected(kind, "", attrs...)
	return domain.ErrInvalidIDToken
}

// tokenRejected — «identity token rejected» ескертуі. reason бос болса, ол attrs ішінде.
func (s *Service) tokenRejected(kind, reason string, attrs ...any) {
	fields := []any{"provider", kind}
	if reason != "" {
		fields = append(fields, "reason", reason)
	}
	s.log.Warn("identity token rejected", append(fields, attrs...)...)
}

// nonceForm — nonce келісімі қалай бұзылды (мәннің өзі журналға шықпайды).
//
// Google must carry the app's nonce as is; Apple must carry the lowercase hex
// SHA-256 of the raw nonce the app sends here. "absent": the token has no
// nonce; "sent_value": the token carries exactly what the app sent here (for
// Apple: the app put the raw value in the Apple request, or sent us the hash
// instead of the raw value); "hashed": a Google token carries the hash;
// "uppercase_hash": the Apple hash in upper case; "different": another value,
// e.g. the nonce of an earlier attempt.
func nonceForm(kind, sent string, rejected *idtoken.RejectError) string {
	switch {
	case rejected.NonceIs(""):
		return "absent"
	case rejected.NonceIs(sent):
		return "sent_value"
	case kind == domain.IdentityGoogle && rejected.NonceIs(idtoken.AppleNonce(sent)):
		return "hashed"
	case kind == domain.IdentityApple && rejected.NonceIs(strings.ToUpper(idtoken.AppleNonce(sent))):
		return "uppercase_hash"
	default:
		return "different"
	}
}

func (s *Service) signInWithProvider(ctx context.Context, p providerSignIn, info DeviceInfo) (Session, error) {
	user, err := s.repo.UserByAuthIdentity(ctx, p.kind, p.subject)
	isNew := false
	switch {
	case err == nil:
		_ = s.repo.RefreshIdentity(ctx, p.kind, p.subject, p.email, p.emailVerified)
		s.touchUser(ctx, user, info)
	case errors.Is(err, domain.ErrNotFound):
		if user, isNew, err = s.linkOrCreateProviderUser(ctx, p, info); err != nil {
			return Session{}, err
		}
	default:
		return Session{}, err
	}
	if p.displayName != "" {
		_ = s.repo.SetDisplayNameIfEmpty(ctx, user.ID, p.displayName)
	}
	return s.openSession(ctx, user, isNew, info)
}

// linkOrCreateProviderUser — бізге жаңа провайдер тіркелгісі: бар тіркелгіге қосу не жаңасын ашу.
//
// The rule is the one e-mail sign-in already follows: proving control of a
// mailbox gives access to the account that holds it. A provider proves it
// only for addresses it is authoritative for (googleAuthoritative,
// appleAuthoritative), so only those link to an existing account. Any other
// provider address that already belongs to an account is refused with
// ErrEmailInUse — the user signs in with e-mail instead — rather than opening
// a second account or taking over the first. A duplicate can confuse; a
// taken-over account cannot be given back.
func (s *Service) linkOrCreateProviderUser(ctx context.Context, p providerSignIn, info DeviceInfo) (domain.User, bool, error) {
	now := s.clock.Now()
	identity := domain.Identity{
		Kind: p.kind, Value: p.subject, ProviderEmail: p.email,
		ProviderEmailVerified: p.emailVerified, VerifiedAt: &now,
	}

	if p.emailTrusted {
		existing, err := s.accountHoldingEmail(ctx, p.email)
		switch {
		case err == nil:
			if err := s.repo.LinkIdentity(ctx, existing.ID, identity); err != nil && !errors.Is(err, domain.ErrConflict) {
				return domain.User{}, false, err
			}
			// On a conflict a parallel request linked it first; the subject decides.
			linked, err := s.repo.UserByAuthIdentity(ctx, p.kind, p.subject)
			if err != nil {
				return domain.User{}, false, err
			}
			_ = s.repo.ConfirmEmail(ctx, linked.ID, p.email, now)
			s.log.Info("identity linked to existing account", "provider", p.kind)
			s.touchUser(ctx, linked, info)
			return linked, false, nil
		case !errors.Is(err, domain.ErrNotFound):
			return domain.User{}, false, err
		}
	}

	// Every account keeps an address it can be recovered with, and the schema
	// requires one (users CHECK). Providers send a verified address whenever the
	// e-mail scope is granted, which both apps request.
	if !p.emailVerified {
		// email_in_token: the token had an address the provider did not mark verified.
		s.tokenRejected(p.kind, "missing_email", "email_in_token", p.email != "")
		return domain.User{}, false, domain.ErrInvalidIDToken
	}
	identities := []domain.Identity{identity}
	var verifiedAt *time.Time
	if p.emailTrusted {
		verifiedAt = &now
		identities = append(identities, domain.Identity{
			Kind: domain.IdentityEmail, Value: p.email, ProviderEmail: p.email,
			ProviderEmailVerified: true, VerifiedAt: &now,
		})
	}
	created, err := s.repo.CreateUserWithIdentities(ctx, s.newUser(info, p.email), verifiedAt, identities...)
	if errors.Is(err, domain.ErrConflict) {
		// A parallel first sign-in with the same provider account won the race…
		if user, lookupErr := s.repo.UserByAuthIdentity(ctx, p.kind, p.subject); lookupErr == nil {
			return user, false, nil
		}
		// …or the address belongs to an account this provider cannot vouch for.
		return domain.User{}, false, domain.ErrEmailInUse
	}
	if err != nil {
		return domain.User{}, false, err
	}
	if err := s.provision(ctx, created, info); err != nil {
		return domain.User{}, false, err
	}
	return created, true, nil
}

// googleAuthoritative — Google осы мекенжай үшін беделді ме.
//
// Google's own rule: it is authoritative for @gmail.com addresses and for
// Google Workspace accounts (the `hd` claim, which must match the address).
// For any other address `email_verified` only says Google checked it once,
// when the account was made; ownership may have changed since.
func googleAuthoritative(address, hostedDomain string) bool {
	if strings.HasSuffix(address, "@gmail.com") || strings.HasSuffix(address, "@googlemail.com") {
		return true
	}
	hd := strings.ToLower(strings.TrimSpace(hostedDomain))
	return hd != "" && strings.HasSuffix(address, "@"+hd)
}

// appleAuthoritative — Apple-дың өз домендері және Hide My Email мекенжайлары.
//
// Apple runs the mailboxes at icloud.com, me.com and mac.com, and the private
// relay addresses it issues per app. Other Apple ID addresses are treated like
// Google's non-authoritative ones.
func appleAuthoritative(address string, private bool) bool {
	if private {
		return strings.HasSuffix(address, "@privaterelay.appleid.com")
	}
	for _, domain := range []string{"@icloud.com", "@me.com", "@mac.com"} {
		if strings.HasSuffix(address, domain) {
			return true
		}
	}
	return false
}

// cleanDisplayName — көрсетілетін атты тазалайды: басқару таңбаларсыз, 80 таңбаға дейін.
func cleanDisplayName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, name)
	return traits.Clamp(traits.CollapseSpaces(name), 80)
}
