// Package appstore — App Store қолтаңбалы деректерін (JWS) тексеру: StoreKit 2 транзакциясы және
// App Store Server Notifications V2.
//
// Apple signs every transaction and notification as a JWS (ES256) whose x5c
// header carries the signing certificate, Apple's intermediate and Apple Root
// CA - G3. The server trusts only the root embedded in this package, pinned by
// its SHA-256 fingerprint: the chain must lead to exactly that root, be valid
// at the payload's signedDate, and carry Apple's marker extensions (leaf
// 1.2.840.113635.100.6.11.1, intermediate 1.2.840.113635.100.6.2.1); the
// signature is the raw r||s ECDSA P-256 signature over "header.payload".
// Only the standard library is used, as in auth/idtoken. No call to Apple is
// made: a signed transaction verifies on its own.
package appstore

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	_ "embed"
)

// AppleRootFingerprint — Apple Root CA - G3 сертификатының SHA-256 саусақ ізі (DER).
//
// Published at https://www.apple.com/certificateauthority/ and checked
// against the copy exported from the macOS system roots
// (security find-certificate -c "Apple Root CA - G3" -p
// /System/Library/Keychains/SystemRootCertificates.keychain).
const AppleRootFingerprint = "63343abfb89a6a03ebb57e9b3f5fa7be7c4f5c756f3017b3a8c488c3653e9179"

//go:embed apple_root_ca_g3.pem
var appleRootPEM []byte

// Apple сертификаттарының белгі кеңейтімдері.
var (
	// OIDLeaf — App Store receipt signing (leaf).
	OIDLeaf = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
	// OIDIntermediate — Apple Worldwide Developer Relations (intermediate).
	OIDIntermediate = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
)

// maxJWSBytes — бұдан ұзын мәтін тексерусіз қабылданбайды (нақты JWS ~5 КБ).
const maxJWSBytes = 64 << 10

// Себептер (Reject.Reason).
const (
	ReasonMalformed = "malformed"
	ReasonAlgorithm = "algorithm"
	ReasonChain     = "chain"
	ReasonSignature = "signature"
)

// ErrInvalid — JWS тексеруден өтпеді (себебі — *Reject).
var ErrInvalid = errors.New("appstore: invalid signed data")

// Reject — қабылданбау себебі. Мәтінде JWS-тің өзі ешқашан болмайды.
type Reject struct {
	Reason string
	Check  string
}

func (r *Reject) Error() string { return ErrInvalid.Error() + ": " + r.Check }

// Unwrap — ErrInvalid.
func (r *Reject) Unwrap() error { return ErrInvalid }

func reject(reason, check string) error { return &Reject{Reason: reason, Check: check} }

// AppleRoot — кіріктірілген Apple Root CA - G3. Саусақ ізі сәйкес келмесе қате: тексеру басталмайды.
func AppleRoot() (*x509.Certificate, error) {
	block, _ := pem.Decode(appleRootPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("appstore: the embedded Apple root is not a PEM certificate")
	}
	sum := sha256.Sum256(block.Bytes)
	if got := hex.EncodeToString(sum[:]); got != AppleRootFingerprint {
		return nil, fmt.Errorf("appstore: the embedded Apple root has fingerprint %s, want %s", got, AppleRootFingerprint)
	}
	return x509.ParseCertificate(block.Bytes)
}

// Verifier — Apple JWS тексерушісі.
type Verifier struct {
	root *x509.Certificate
}

// Option — тексерушінің баптауы.
type Option func(*Verifier)

// WithRoot — тек тесттерге: генерацияланған тізбектің түбірі (кіріктірілген түбірдің орнына).
func WithRoot(root *x509.Certificate) Option { return func(v *Verifier) { v.root = root } }

// New — тексеруші. Әдепкі түбір — кіріктірілген, бекітілген Apple Root CA - G3.
func New(opts ...Option) (*Verifier, error) {
	v := &Verifier{}
	for _, opt := range opts {
		opt(v)
	}
	if v.root == nil {
		root, err := AppleRoot()
		if err != nil {
			return nil, err
		}
		v.root = root
	}
	return v, nil
}

// Transaction — JWSTransactionDecodedPayload: серверге керек өрістер.
type Transaction struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`
	ExpiresDate           int64  `json:"expiresDate"`
	Price                 int64  `json:"price"`
	Currency              string `json:"currency"`
	IsUpgraded            bool   `json:"isUpgraded"`
	Quantity              int    `json:"quantity"`
	// Type — Consumable | Non-Consumable | Auto-Renewable Subscription | Non-Renewing Subscription.
	Type string `json:"type"`
	// AppAccountToken — purchase(options: [.appAccountToken(UUID)]) берген UUID.
	AppAccountToken    string `json:"appAccountToken"`
	InAppOwnershipType string `json:"inAppOwnershipType"`
	SignedDate         int64  `json:"signedDate"`
	RevocationDate     int64  `json:"revocationDate"`
	RevocationReason   *int   `json:"revocationReason"`
	// Environment — Production | Sandbox (Xcode тізбегі Apple түбіріне жетпейді).
	Environment       string `json:"environment"`
	TransactionReason string `json:"transactionReason"`
}

// App Store транзакция түрлері (Transaction.Type).
const (
	TypeConsumable    = "Consumable"
	TypeNonConsumable = "Non-Consumable"
	TypeAutoRenewable = "Auto-Renewable Subscription"
	TypeNonRenewing   = "Non-Renewing Subscription"
)

// Орталар (Transaction.Environment, NotificationData.Environment).
const (
	EnvironmentProduction = "Production"
	EnvironmentSandbox    = "Sandbox"
)

// Сервер хабарламаларының түрлері (Notification.NotificationType), сервер өңдейтіндері.
const (
	NotificationRefund            = "REFUND"
	NotificationRevoke            = "REVOKE"
	NotificationRefundReversed    = "REFUND_REVERSED"
	NotificationRefundDeclined    = "REFUND_DECLINED"
	NotificationConsumption       = "CONSUMPTION_REQUEST"
	NotificationTest              = "TEST"
	NotificationSubscribed        = "SUBSCRIBED"
	NotificationRenewed           = "DID_RENEW"
	NotificationFailedRenewal     = "DID_FAIL_TO_RENEW"
	NotificationExpired           = "EXPIRED"
	NotificationGraceExpired      = "GRACE_PERIOD_EXPIRED"
	NotificationRenewalStatus     = "DID_CHANGE_RENEWAL_STATUS"
	NotificationRenewalPreference = "DID_CHANGE_RENEWAL_PREF"
)

// Notification — ResponseBodyV2DecodedPayload (App Store Server Notifications V2).
type Notification struct {
	NotificationType string           `json:"notificationType"`
	Subtype          string           `json:"subtype"`
	NotificationUUID string           `json:"notificationUUID"`
	Version          string           `json:"version"`
	SignedDate       int64            `json:"signedDate"`
	Data             NotificationData `json:"data"`
}

// NotificationData — хабарламаның data бөлігі.
type NotificationData struct {
	AppAppleID            int64  `json:"appAppleId"`
	BundleID              string `json:"bundleId"`
	BundleVersion         string `json:"bundleVersion"`
	Environment           string `json:"environment"`
	SignedTransactionInfo string `json:"signedTransactionInfo"`
	SignedRenewalInfo     string `json:"signedRenewalInfo"`
}

type Renewal struct {
	OriginalTransactionID  string `json:"originalTransactionId"`
	AutoRenewStatus        int    `json:"autoRenewStatus"`
	GracePeriodExpiresDate int64  `json:"gracePeriodExpiresDate"`
	Environment            string `json:"environment"`
	SignedDate             int64  `json:"signedDate"`
}

func (v *Verifier) Renewal(jws string) (Renewal, error) {
	var r Renewal
	if err := v.Verify(jws, &r); err != nil {
		return r, err
	}
	if r.OriginalTransactionID == "" {
		return r, reject(ReasonMalformed, "renewal without original transaction")
	}
	return r, nil
}

// Transaction — signedTransaction (jwsRepresentation) тексеріп, ашады.
func (v *Verifier) Transaction(jws string) (Transaction, error) {
	var t Transaction
	if err := v.Verify(jws, &t); err != nil {
		return Transaction{}, err
	}
	if t.TransactionID == "" || t.ProductID == "" || t.BundleID == "" {
		return Transaction{}, reject(ReasonMalformed, "transaction without id, product or bundle")
	}
	return t, nil
}

// Notification — signedPayload тексеріп, ашады.
func (v *Verifier) Notification(jws string) (Notification, error) {
	var n Notification
	if err := v.Verify(jws, &n); err != nil {
		return Notification{}, err
	}
	if n.NotificationUUID == "" || n.NotificationType == "" {
		return Notification{}, reject(ReasonMalformed, "notification without uuid or type")
	}
	return n, nil
}

// Verify — қолтаңба мен тізбекті тексеріп, payload-ты into-ға ашады.
func (v *Verifier) Verify(jws string, into any) error {
	jws = strings.TrimSpace(jws)
	if jws == "" || len(jws) > maxJWSBytes {
		return reject(ReasonMalformed, "empty or oversized")
	}
	parts := strings.Split(jws, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return reject(ReasonMalformed, "not a compact JWS")
	}
	var header struct {
		Alg string   `json:"alg"`
		X5C []string `json:"x5c"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return reject(ReasonMalformed, "header")
	}
	if header.Alg != "ES256" {
		return reject(ReasonAlgorithm, "alg is not ES256")
	}
	if len(header.X5C) != 3 {
		return reject(ReasonChain, "x5c must hold the leaf, the intermediate and the root")
	}
	certs := make([]*x509.Certificate, 0, 3)
	for _, raw := range header.X5C {
		der, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return reject(ReasonChain, "x5c entry is not base64")
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return reject(ReasonChain, "x5c entry is not a certificate")
		}
		certs = append(certs, cert)
	}
	leaf, intermediate, root := certs[0], certs[1], certs[2]
	if !bytes.Equal(root.Raw, v.root.Raw) {
		return reject(ReasonChain, "the chain does not end at the Apple root")
	}

	var dated struct {
		SignedDate int64 `json:"signedDate"`
	}
	if err := decodeSegment(parts[1], &dated); err != nil {
		return reject(ReasonMalformed, "payload")
	}
	if dated.SignedDate <= 0 {
		return reject(ReasonMalformed, "payload without signedDate")
	}
	signed := time.UnixMilli(dated.SignedDate).UTC()

	// The Apple markers are checked here; when a CA marks them critical,
	// x509 would otherwise refuse the chain for an extension it does not know.
	markHandled(leaf, OIDLeaf)
	markHandled(intermediate, OIDIntermediate)
	if !hasExtension(leaf, OIDLeaf) {
		return reject(ReasonChain, "the leaf is not an App Store signing certificate")
	}
	if !hasExtension(intermediate, OIDIntermediate) {
		return reject(ReasonChain, "the intermediate is not Apple's")
	}
	roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(v.root)
	intermediates.AddCert(intermediate)
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, CurrentTime: signed,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return reject(ReasonChain, "certificate chain: "+err.Error())
	}
	if !throughIntermediate(chains, intermediate, v.root) {
		return reject(ReasonChain, "the chain does not pass through the given intermediate")
	}

	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return reject(ReasonSignature, "the leaf key is not P-256")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return reject(ReasonSignature, "the signature is not 64 raw bytes")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return reject(ReasonSignature, "bad signature")
	}
	if err := decodeSegment(parts[1], into); err != nil {
		return reject(ReasonMalformed, "payload")
	}
	return nil
}

func decodeSegment(segment string, target any) error {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segment, "="))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func hasExtension(cert *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oid) {
			return true
		}
	}
	return false
}

func markHandled(cert *x509.Certificate, oid asn1.ObjectIdentifier) {
	kept := cert.UnhandledCriticalExtensions[:0]
	for _, id := range cert.UnhandledCriticalExtensions {
		if !id.Equal(oid) {
			kept = append(kept, id)
		}
	}
	cert.UnhandledCriticalExtensions = kept
}

// throughIntermediate — тексерілген тізбек дәл leaf → берілген intermediate → түбір.
func throughIntermediate(chains [][]*x509.Certificate, intermediate, root *x509.Certificate) bool {
	for _, chain := range chains {
		if len(chain) == 3 && chain[1].Equal(intermediate) && chain[2].Equal(root) {
			return true
		}
	}
	return false
}
