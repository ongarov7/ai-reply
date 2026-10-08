package idtoken

import (
	"crypto/subtle"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Reason — токен неге қабылданбады: журналға жазылатын тұрақты санат.
type Reason string

const (
	// ReasonMalformed — JWT емес, base64/JSON бұзық, тым ұзын, sub не exp жоқ.
	ReasonMalformed Reason = "malformed"
	// ReasonAlgorithm — RS256 емес.
	ReasonAlgorithm Reason = "unsupported_algorithm"
	// ReasonKeyID — kid жоқ не провайдердің жариялаған кілттерінде жоқ.
	ReasonKeyID Reason = "unknown_key_id"
	// ReasonSignature — қолтаңба провайдердің кілтіне сәйкес емес.
	ReasonSignature Reason = "bad_signature"
	// ReasonIssuer — iss біз күткен провайдер емес.
	ReasonIssuer Reason = "issuer_mismatch"
	// ReasonAudience — aud біздің client id-лердің ешқайсысы емес (APPLE_CLIENT_ID, GOOGLE_CLIENT_ID_*).
	ReasonAudience Reason = "audience_mismatch"
	// ReasonExpired — exp өтіп кеткен (допусктан кейін).
	ReasonExpired Reason = "expired"
	// ReasonNotYetValid — iat не nbf сервер сағатынан алда (сағат айырмашылығы).
	ReasonNotYetValid Reason = "not_yet_valid"
	// ReasonNonce — nonce осы кіру әрекетінікі емес.
	ReasonNonce Reason = "nonce_mismatch"
)

// RejectError — қабылданбаған токен: себеп санаты және тексеруге керек ашық мәндер.
//
// It wraps ErrInvalidToken, so errors.Is keeps working. The exported fields
// hold only values that are public by design — the issuer, the client ids in
// aud/azp, the key id — and the client ids this server is configured with.
// The token itself, its subject, its e-mail and its nonce are never part of
// Attrs, so the whole set can go to the log as it is.
type RejectError struct {
	Reason Reason
	// Check — тексерудің өз сөзі ("audience mismatch", "missing expiry").
	Check string
	// Issuer, Audience, AuthorizedParty — токендегі iss, aud, azp (iss/aud тексерісі құлағанда).
	Issuer          string
	Audience        []string
	AuthorizedParty string
	// Expected — бапталған мәндер: iss тексерісінде провайдердің iss-і, aud тексерісінде client id-лер.
	Expected []string
	// KeyID — токен атаған kid (kid не қолтаңба тексерісі құлағанда).
	KeyID string
	// Off — expired: exp-тен бері қанша өтті; not_yet_valid: токен уақыты қанша алда.
	Off time.Duration
	// tokenNonce — тек nonce_mismatch-та: клиент келісімін салыстыру үшін (журналға шықпайды).
	tokenNonce string
}

func (e *RejectError) Error() string { return ErrInvalidToken.Error() + ": " + e.Check }

// Unwrap — errors.Is(err, ErrInvalidToken).
func (e *RejectError) Unwrap() error { return ErrInvalidToken }

// NonceIs — токендегі nonce осы мәнге тең бе (тек nonce_mismatch-та; тұрақты уақытта).
//
// Lets the caller tell how a client broke the nonce contract (no nonce at all,
// the raw value where its hash belongs, …) without the nonce leaving here.
func (e *RejectError) NonceIs(value string) bool {
	return e.Reason == ReasonNonce && subtle.ConstantTimeCompare([]byte(e.tokenNonce), []byte(value)) == 1
}

// Attrs — журнал өрістері (slog кілт–мән жұптары): санат және оған қатысты ашық мәндер.
func (e *RejectError) Attrs() []any {
	attrs := []any{"reason", string(e.Reason), "check", e.Check}
	switch e.Reason {
	case ReasonIssuer:
		attrs = append(attrs, "token_iss", clampValue(e.Issuer), "configured_iss", e.Expected)
	case ReasonAudience:
		attrs = append(attrs, "token_aud", clampValues(e.Audience), "configured_aud", e.Expected)
		if e.AuthorizedParty != "" {
			attrs = append(attrs, "token_azp", clampValue(e.AuthorizedParty))
		}
	case ReasonKeyID, ReasonSignature:
		if e.KeyID != "" {
			attrs = append(attrs, "kid", clampValue(e.KeyID))
		}
	case ReasonExpired, ReasonNotYetValid:
		if e.Off > 0 {
			attrs = append(attrs, "seconds", int64(e.Off/time.Second))
		}
	}
	return attrs
}

func reject(reason Reason, check string) *RejectError {
	return &RejectError{Reason: reason, Check: check}
}

// clampValue, clampValues — токеннен алынған мәндер журналды толтырмасын.
func clampValue(v string) string { return traits.Clamp(v, 200) }

func clampValues(values []string) []string {
	if len(values) > 5 {
		values = values[:5]
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = clampValue(v)
	}
	return out
}
