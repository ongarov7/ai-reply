// Package auth — кіру, OTP, токендер.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims — access токеннің мазмұны. Дербес дерек жоқ: тек UUID-тер.
type Claims struct {
	Subject              string `json:"sub"`
	Issuer               string `json:"iss"`
	IssuedAt             int64  `json:"iat"`
	ExpiresAt            int64  `json:"exp"`
	TokenID              string `json:"jti"`
	Type                 string `json:"typ"`
	DeviceID             string `json:"did,omitempty"`
	Platform             string `json:"plt,omitempty"`
	InstallationIdentity string `json:"iid,omitempty"`
}

var (
	// ErrTokenInvalid — қолтаңба не пішім қате.
	ErrTokenInvalid = errors.New("auth: token invalid")
	// ErrTokenExpired — мерзімі өткен.
	ErrTokenExpired = errors.New("auth: token expired")
)

// SignJWT — HS256 (стандартты кітапхана ғана, сыртқы тәуелділік жоқ).
func SignJWT(secret string, claims Claims) (string, error) {
	header := base64url([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64url(body)
	signing := header + "." + payload
	return signing + "." + base64url(sign(secret, signing)), nil
}

// ParseJWT — қолтаңбаны және мерзімді тексереді.
func ParseJWT(secret, token string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrTokenInvalid
	}
	expected := base64url(sign(secret, parts[0]+"."+parts[1]))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return Claims{}, ErrTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	var claims Claims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return Claims{}, ErrTokenInvalid
	}
	if claims.ExpiresAt > 0 && now.UTC().Unix() >= claims.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}
	return claims, nil
}

func sign(secret, value string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func base64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// HashToken — refresh/сессия токенін сақтауға арналған SHA-256 хэші.
func HashToken(secret, token string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// InstallationIdentity binds a session to an explicitly supplied installation,
// without copying its raw identifier into JWTs or refresh-token records.
func InstallationIdentity(secret, installationID string) string {
	if installationID == "" {
		return ""
	}
	return HashToken(secret, "ai-reply/pro-installation/v1\x00"+installationID)
}
