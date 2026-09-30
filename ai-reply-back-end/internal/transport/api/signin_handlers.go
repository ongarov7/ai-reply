package api

import (
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Кіру: пошта OTP (Resend), Google, Apple және пошта қосу.

type emailOTPRequest struct {
	Email  string `json:"email"`
	Locale string `json:"locale"`
}

type emailChallengeResponse struct {
	MaskedEmail string `json:"masked_email"`
	ExpiresIn   int    `json:"expires_in"`
	ResendAfter int    `json:"resend_after"`
	CodeLength  int    `json:"code_length"`
	DemoMode    bool   `json:"demo_mode,omitempty"`
}

func toEmailChallenge(c auth.EmailChallenge) emailChallengeResponse {
	return emailChallengeResponse{
		MaskedEmail: c.MaskedEmail,
		ExpiresIn:   c.ExpiresIn,
		ResendAfter: c.ResendAfter,
		CodeLength:  c.CodeLength,
		DemoMode:    c.DemoMode,
	}
}

// handleEmailOTPRequest — поштаға 4 таңбалы код жібереді.
//
// The answer is identical for known and unknown addresses; only the resend
// cooldown and the rate limits can refuse, and they say when to try again.
func (s *Server) handleEmailOTPRequest(w http.ResponseWriter, r *http.Request) {
	var body emailOTPRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	challenge, err := s.auth.RequestEmailOTP(r.Context(), body.Email, traits.Clamp(body.Locale, 8))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toEmailChallenge(challenge))
}

type emailOTPVerifyRequest struct {
	Email  string        `json:"email"`
	Code   string        `json:"code"`
	Device deviceRequest `json:"device"`
}

// handleEmailOTPVerify — кодты тексеріп, тіркелгіні табады не ашады және сессия береді.
func (s *Server) handleEmailOTPVerify(w http.ResponseWriter, r *http.Request) {
	var body emailOTPVerifyRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	session, err := s.auth.VerifyEmailOTP(r.Context(), body.Email, body.Code, body.Device.toInfo(r))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.writeSession(w, r, session)
}

type googleSignInRequest struct {
	IDToken string        `json:"id_token"`
	Nonce   string        `json:"nonce"`
	Device  deviceRequest `json:"device"`
}

// handleGoogleSignIn — Google ID token-ді серверде тексеріп, сессия береді.
func (s *Server) handleGoogleSignIn(w http.ResponseWriter, r *http.Request) {
	var body googleSignInRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	session, err := s.auth.SignInWithGoogle(r.Context(), body.IDToken, body.Nonce, body.Device.toInfo(r))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.writeSession(w, r, session)
}

type appleSignInRequest struct {
	IdentityToken string        `json:"identity_token"`
	Nonce         string        `json:"nonce"`
	FullName      string        `json:"full_name"`
	Device        deviceRequest `json:"device"`
}

// handleAppleSignIn — Apple identity token-ді серверде тексеріп, сессия береді.
func (s *Server) handleAppleSignIn(w http.ResponseWriter, r *http.Request) {
	var body appleSignInRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	session, err := s.auth.SignInWithApple(r.Context(), body.IdentityToken, body.Nonce,
		traits.Clamp(body.FullName, 120), body.Device.toInfo(r))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.writeSession(w, r, session)
}

// handleLinkEmailRequest — кірген тіркелгіге пошта қосу коды.
func (s *Server) handleLinkEmailRequest(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body emailOTPRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	challenge, err := s.auth.RequestLinkEmailOTP(r.Context(), user, body.Email, traits.Clamp(body.Locale, 8))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toEmailChallenge(challenge))
}

type linkEmailVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// handleLinkEmailVerify — кодты тексеріп, поштаны тіркелгіге бекітеді; жаңартылған /me қайтады.
func (s *Server) handleLinkEmailVerify(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body linkEmailVerifyRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	updated, err := s.auth.VerifyLinkEmailOTP(r.Context(), user, body.Email, body.Code)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.writeMe(w, r, updated)
}
