package bcc

import (
	"crypto/hmac"
	"crypto/sha1" // BCC's documented request MAC algorithm.
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
)

// SignRequest computes P_SIGN for a documented request, preserving its exact
// field values. It neither normalizes amounts nor verifies response signatures.
// Required signed fields must be present, nonempty and ASCII; their character
// lengths therefore equal their UTF-8 byte lengths. Unknown TRTYPEs fail closed.
func SignRequest(kind TransactionType, fields url.Values, keyHex string) (string, error) {
	key, err := decodeMACKey(keyHex)
	if err != nil {
		return "", err
	}
	return signRequest(kind, fields, key)
}

func decodeMACKey(keyHex string) ([]byte, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) < 16 || len(key) > 64 {
		return nil, ErrInvalidKey
	}
	return key, nil
}

func signedFields(kind TransactionType) ([]string, error) {
	switch kind {
	case Purchase:
		return []string{"AMOUNT", "CURRENCY", "ORDER", "MERCHANT", "TERMINAL", "MERCH_GMT", "TIMESTAMP", "TRTYPE", "NONCE"}, nil
	case Refund, Reversal:
		return []string{"ORDER", "ORG_AMOUNT", "AMOUNT", "CURRENCY", "RRN", "INT_REF", "TERMINAL", "TIMESTAMP", "TRTYPE", "NONCE"}, nil
	case CreateToken:
		return []string{"ORDER", "RRN", "INT_REF", "MERCH_GMT", "TIMESTAMP", "TRTYPE", "TERMINAL", "NONCE"}, nil
	case DeleteToken:
		return []string{"MERCH_TOKEN_ID", "ORDER", "MERCH_GMT", "TIMESTAMP", "TRTYPE", "TERMINAL", "NONCE"}, nil
	case StatusQuery:
		return []string{"ORDER", "TERMINAL", "TIMESTAMP", "TRTYPE", "NONCE"}, nil
	case RecurringCharge:
		return []string{"ORDER", "AMOUNT", "CURRENCY", "INT_REF", "TERMINAL", "TIMESTAMP", "TRTYPE", "RECUR_REF", "NONCE"}, nil
	default:
		return nil, ErrUnsupportedTransaction
	}
}

func signRequest(kind TransactionType, fields url.Values, key []byte) (string, error) {
	names, err := signedFields(kind)
	if err != nil {
		return "", err
	}
	if fields.Get("TRTYPE") != string(kind) || len(fields) > maxFields {
		return "", ErrInvalidRequest
	}
	for name, values := range fields {
		if len(name) == 0 || len(name) > maxFieldName || len(values) != 1 || len(values[0]) > maxFieldValue {
			return "", ErrInvalidRequest
		}
	}
	var source strings.Builder
	for _, name := range names {
		value := fields.Get(name)
		if value == "" || !printableASCII(value) {
			return "", ErrInvalidRequest
		}
		source.WriteString(strconv.Itoa(len(value)))
		source.WriteString(value)
	}
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write([]byte(source.String()))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil))), nil
}
