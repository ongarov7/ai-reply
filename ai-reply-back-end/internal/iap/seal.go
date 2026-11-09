package iap

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// sealer — Google purchase token-ді қажет кезде AES-256-GCM-мен шифрлап сақтайды.
//
// The consume retry and the subscription worker need the token after the
// request is gone; the database keeps it sealed with a key derived from a
// server secret (like push tokens): a one-time token until it is consumed, a
// subscription's for as long as it lives. A copy of the database file alone
// never reveals a token. The first key seals and every key opens, so a key can
// be added without losing what an older one sealed.
type sealer struct {
	aeads []cipher.AEAD
}

const sealPrefix = "v1:"

var errUnseal = errors.New("iap: purchase token cannot be opened")

func newSealer(keys ...[]byte) *sealer {
	s := &sealer{}
	for _, key := range keys {
		block, err := aes.NewCipher(key)
		if err != nil {
			panic("iap: aes: " + err.Error())
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			panic("iap: gcm: " + err.Error())
		}
		s.aeads = append(s.aeads, aead)
	}
	return s
}

func (s *sealer) seal(token string) (string, error) {
	aead := s.aeads[0]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := aead.Seal(nonce, nonce, []byte(token), []byte(sealPrefix))
	return sealPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

func (s *sealer) open(sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealPrefix) {
		return "", errUnseal
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil {
		return "", errUnseal
	}
	for _, aead := range s.aeads {
		if len(raw) < aead.NonceSize() {
			continue
		}
		if plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(sealPrefix)); err == nil {
			return string(plain), nil
		}
	}
	return "", errUnseal
}
