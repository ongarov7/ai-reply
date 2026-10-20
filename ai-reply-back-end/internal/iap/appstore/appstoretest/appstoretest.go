// Package appstoretest — тесттерге арналған жалған Apple сертификат тізбегі және JWS қолтаңбасы.
//
// The chain has Apple's shape: a self-signed root, an intermediate with the
// 1.2.840.113635.100.6.2.1 marker and a P-256 leaf with
// 1.2.840.113635.100.6.11.1. Tests inject Root through appstore.WithRoot and
// sign transactions and notifications exactly as the App Store does
// (ES256, x5c, raw r||s signature). Nothing here is trusted in production.
package appstoretest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"
)

var (
	oidLeaf         = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
	oidIntermediate = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
)

// Options — тізбектің ауытқулары (жарамсыз тізбек тесттері үшін).
type Options struct {
	// NoLeafOID, NoIntermediateOID — Apple белгісі жоқ сертификат.
	NoLeafOID         bool
	NoIntermediateOID bool
	// CriticalOIDs — Apple белгілері critical деп белгіленеді.
	CriticalOIDs bool
	// NotBefore, NotAfter — сертификаттардың мерзімі (бос — 2020…2040).
	NotBefore, NotAfter time.Time
}

// Chain — түбір, аралық және leaf (кілттерімен).
type Chain struct {
	Root, Intermediate, Leaf *x509.Certificate
	LeafKey                  *ecdsa.PrivateKey
}

// NewChain — жаңа тізбек.
func NewChain(t testing.TB, opts Options) *Chain {
	t.Helper()
	notBefore, notAfter := opts.NotBefore, opts.NotAfter
	if notBefore.IsZero() {
		notBefore = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if notAfter.IsZero() {
		notAfter = time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	key := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	ext := func(oid asn1.ObjectIdentifier) []pkix.Extension {
		return []pkix.Extension{{Id: oid, Critical: opts.CriticalOIDs, Value: []byte{0x05, 0x00}}}
	}
	issue := func(tpl, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, tpl, parent, pub, signer)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}

	rootKey, interKey, leafKey := key(), key(), key()
	rootTpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Apple Root CA - G3"},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	root := issue(rootTpl, rootTpl, &rootKey.PublicKey, rootKey)

	interTpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Test Apple Worldwide Developer Relations CA - G6"},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	if !opts.NoIntermediateOID {
		interTpl.ExtraExtensions = ext(oidIntermediate)
	}
	intermediate := issue(interTpl, root, &interKey.PublicKey, rootKey)

	leafTpl := &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "Test Prod ECC Mac App Store and iTunes Store Receipt Signing"},
		NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	if !opts.NoLeafOID {
		leafTpl.ExtraExtensions = ext(oidLeaf)
	}
	leaf := issue(leafTpl, intermediate, &leafKey.PublicKey, interKey)
	return &Chain{Root: root, Intermediate: intermediate, Leaf: leaf, LeafKey: leafKey}
}

// X5C — header-дің x5c өрісі (standard base64 DER).
func (c *Chain) X5C() []string {
	return []string{
		base64.StdEncoding.EncodeToString(c.Leaf.Raw),
		base64.StdEncoding.EncodeToString(c.Intermediate.Raw),
		base64.StdEncoding.EncodeToString(c.Root.Raw),
	}
}

// Sign — App Store сияқты ES256 JWS.
func (c *Chain) Sign(t testing.TB, payload any) string {
	t.Helper()
	return c.SignWithHeader(t, map[string]any{"alg": "ES256", "x5c": c.X5C()}, payload)
}

// SignWithHeader — ерікті header (alg none, HS256, басқа x5c); ES256 болмаса HMAC не бос қолтаңба.
func (c *Chain) SignWithHeader(t testing.TB, header map[string]any, payload any) string {
	t.Helper()
	segment := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signing := segment(header) + "." + segment(payload)
	switch header["alg"] {
	case "ES256":
		digest := sha256.Sum256([]byte(signing))
		r, s, err := ecdsa.Sign(rand.Reader, c.LeafKey, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		sig := make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
		return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
	case "HS256":
		mac := hmac.New(sha256.New, []byte("guessable-secret"))
		mac.Write([]byte(signing))
		return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	default:
		return signing + ".AA"
	}
}

// Transaction — JWSTransactionDecodedPayload-тың тестке керек өрістері.
type Transaction struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId,omitempty"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`
	Quantity              int    `json:"quantity"`
	Type                  string `json:"type"`
	AppAccountToken       string `json:"appAccountToken,omitempty"`
	InAppOwnershipType    string `json:"inAppOwnershipType"`
	SignedDate            int64  `json:"signedDate"`
	RevocationDate        int64  `json:"revocationDate,omitempty"`
	RevocationReason      *int   `json:"revocationReason,omitempty"`
	Environment           string `json:"environment"`
	Storefront            string `json:"storefront"`
	Currency              string `json:"currency"`
	Price                 int64  `json:"price"`
}

// Notification — ResponseBodyV2DecodedPayload.
type Notification struct {
	NotificationType string           `json:"notificationType"`
	Subtype          string           `json:"subtype,omitempty"`
	NotificationUUID string           `json:"notificationUUID"`
	Version          string           `json:"version"`
	SignedDate       int64            `json:"signedDate"`
	Data             NotificationData `json:"data"`
}

// NotificationData — data бөлігі.
type NotificationData struct {
	AppAppleID            int64  `json:"appAppleId"`
	BundleID              string `json:"bundleId"`
	BundleVersion         string `json:"bundleVersion"`
	Environment           string `json:"environment"`
	SignedTransactionInfo string `json:"signedTransactionInfo,omitempty"`
}
