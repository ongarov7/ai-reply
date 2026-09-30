// Package httpx — HTTP қабатының ортақ бөлшектері: қате конверті, JSON оқу/жазу.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Қате кодтары — клиент оларды өз тілінде көрсетеді (мәтінге тәуелді емес).
const (
	CodeInvalidRequest  = "INVALID_REQUEST"
	CodeInvalidOTP      = "INVALID_OTP"
	CodeOTPExpired      = "OTP_EXPIRED"
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeTokenExpired    = "TOKEN_EXPIRED"
	CodeAccountDisabled = "ACCOUNT_DISABLED"
	CodeDailyLimit      = "DAILY_LIMIT_REACHED"
	CodeMonthlyLimit    = "MONTHLY_LIMIT_REACHED"
	CodeSubExpired      = "SUBSCRIPTION_EXPIRED"
	CodeRateLimited     = "RATE_LIMITED"
	CodeProviderDown    = "AI_PROVIDER_UNAVAILABLE"
	CodeProviderTimeout = "AI_TIMEOUT"
	CodeEmptyCompletion = "AI_EMPTY_RESPONSE"
	CodePaymentRequired = "PAYMENT_REQUIRED"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeDemoDisabled    = "DEMO_AUTH_DISABLED"
	CodeInternal        = "INTERNAL_ERROR"

	// Кіру: пошта OTP, Google, Apple.
	CodeInvalidEmail        = "INVALID_EMAIL"
	CodeOTPAlreadyUsed      = "OTP_ALREADY_USED"
	CodeOTPAttemptsExceeded = "OTP_ATTEMPTS_EXCEEDED"
	CodeOTPResendCooldown   = "OTP_RESEND_COOLDOWN"
	CodeEmailDelivery       = "EMAIL_DELIVERY_FAILED"
	CodeEmailInUse          = "EMAIL_ALREADY_IN_USE"
	CodeInvalidIDToken      = "INVALID_ID_TOKEN"
	CodeAuthProviderDown    = "AUTH_PROVIDER_UNAVAILABLE"
)

// ErrorBody — қате конверті.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// JSON — сәтті жауап.
func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// Error — тұрақты кодпен қате жауабы. Ішкі мәтін ешқашан клиентке кетпейді.
func Error(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	JSON(w, status, errorEnvelope{Error: ErrorBody{Code: code, Message: message, Details: details}})
}

// detailer — клиентке қосымша мәлімет беретін қате (мысалы, retry_after_seconds).
type detailer interface {
	Details() map[string]any
}

// Fail — домендік қатені HTTP мәртебесі мен кодына айналдырады.
func Fail(w http.ResponseWriter, err error) {
	status, code, message := Translate(err)
	var details map[string]any
	var d detailer
	if errors.As(err, &d) {
		details = d.Details()
		if seconds, ok := details["retry_after_seconds"].(int); ok && seconds > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
	}
	Error(w, status, code, message, details)
}

// Translate — қате → (мәртебе, код, хабар).
func Translate(err error) (int, string, string) {
	switch {
	case errors.Is(err, domain.ErrInvalidEmail):
		return http.StatusBadRequest, CodeInvalidEmail, "The e-mail address is not valid."
	case errors.Is(err, domain.ErrOTPAlreadyUsed):
		return http.StatusBadRequest, CodeOTPAlreadyUsed, "The code has already been used."
	case errors.Is(err, domain.ErrOTPAttemptsExceeded):
		return http.StatusTooManyRequests, CodeOTPAttemptsExceeded, "Too many attempts. Request a new code."
	case errors.Is(err, domain.ErrOTPCooldown):
		return http.StatusTooManyRequests, CodeOTPResendCooldown, "Wait before requesting a new code."
	case errors.Is(err, domain.ErrEmailDelivery):
		return http.StatusServiceUnavailable, CodeEmailDelivery, "The e-mail could not be sent."
	case errors.Is(err, domain.ErrEmailInUse):
		return http.StatusConflict, CodeEmailInUse, "This e-mail belongs to another account."
	case errors.Is(err, domain.ErrInvalidIDToken):
		return http.StatusUnauthorized, CodeInvalidIDToken, "The sign-in token is not valid."
	case errors.Is(err, domain.ErrAuthProviderUnavailable):
		return http.StatusServiceUnavailable, CodeAuthProviderDown, "The sign-in provider is unavailable."
	case errors.Is(err, domain.ErrInvalidOTP):
		return http.StatusBadRequest, CodeInvalidOTP, "The code is not correct."
	case errors.Is(err, domain.ErrOTPExpired):
		return http.StatusBadRequest, CodeOTPExpired, "The code has expired."
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, CodeUnauthorized, "Authentication is required."
	case errors.Is(err, domain.ErrAccountDisabled):
		return http.StatusForbidden, CodeAccountDisabled, "This account is disabled."
	case errors.Is(err, domain.ErrDailyLimit):
		return http.StatusTooManyRequests, CodeDailyLimit, "Daily generation limit reached."
	case errors.Is(err, domain.ErrMonthlyLimit):
		return http.StatusTooManyRequests, CodeMonthlyLimit, "Monthly generation limit reached."
	case errors.Is(err, domain.ErrSubscriptionGone):
		return http.StatusPaymentRequired, CodeSubExpired, "The subscription has expired."
	case errors.Is(err, domain.ErrPaymentRequired):
		return http.StatusPaymentRequired, CodePaymentRequired, "Payment is required."
	case errors.Is(err, domain.ErrRateLimited):
		return http.StatusTooManyRequests, CodeRateLimited, "Too many requests. Try again shortly."
	case errors.Is(err, domain.ErrProviderTimeout):
		return http.StatusGatewayTimeout, CodeProviderTimeout, "The generation timed out."
	case errors.Is(err, domain.ErrProviderDown):
		return http.StatusBadGateway, CodeProviderDown, "The AI provider is unavailable."
	case errors.Is(err, domain.ErrEmptyCompletion):
		return http.StatusBadGateway, CodeEmptyCompletion, "The provider returned no usable reply."
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, CodeNotFound, "Not found."
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, CodeConflict, "Already exists."
	case errors.Is(err, domain.ErrDemoDisabled):
		return http.StatusForbidden, CodeDemoDisabled, "Demo authentication is disabled."
	case errors.Is(err, domain.ErrInvalidRequest):
		return http.StatusBadRequest, CodeInvalidRequest, "The request is not valid."
	default:
		return http.StatusInternalServerError, CodeInternal, "Something went wrong."
	}
}

// Decode — денесі шектелген JSON оқу. Дене ешқашан журналға жазылмайды.
func Decode(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	if maxBytes <= 0 {
		maxBytes = 32 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.ErrInvalidRequest
		}
		return domain.ErrInvalidRequest
	}
	return nil
}

// ClientIP — прокси артында да жұмыс істейтін IP анықтау.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return strings.TrimSpace(real)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
