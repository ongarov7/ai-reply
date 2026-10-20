package iap

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// sealer — Google purchase token-ді тұтынылғанша AES-256-GCM-мен шифрлап сақтайды.
//
// The consume retry needs the token after the request is gone; the database
// keeps it sealed with a key derived from the server secret (like push
// tokens), and only until the purchase is consumed. A copy of the database
// file alone never reveals a token.
type sealer struct {
	aead cipher.AEAD
}

const sealPrefix = "v1:"

var errUnseal = errors.New("iap: purchase token cannot be opened")

func newSealer(key []byte) *sealer {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic("iap: aes: " + err.Error())
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic("iap: gcm: " + err.Error())
	}
	return &sealer{aead: aead}
}

func (s *sealer) seal(token string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, []byte(token), []byte(sealPrefix))
	return sealPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

func (s *sealer) open(sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealPrefix) {
		return "", errUnseal
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", errUnseal
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], []byte(sealPrefix))
	if err != nil {
		return "", errUnseal
	}
	return string(plain), nil
}
