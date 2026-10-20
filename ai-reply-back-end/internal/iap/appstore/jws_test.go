package appstore_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

var signedAt = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func transaction() appstoretest.Transaction {
	return appstoretest.Transaction{
		TransactionID: "2000000812345678", OriginalTransactionID: "2000000812345678",
		BundleID: "kz.ai-reply.reply.keyboard.keyboard", ProductID: "kz.aireply.pack50",
		PurchaseDate: signedAt.UnixMilli(), Quantity: 1, Type: appstore.TypeConsumable,
		AppAccountToken: "6f9c2a1e-3b4d-4c5e-8f60-718293a4b5c6", InAppOwnershipType: "PURCHASED",
		SignedDate: signedAt.UnixMilli(), Environment: appstore.EnvironmentProduction,
		Storefront: "KAZ", Currency: "KZT", Price: 990000,
	}
}

func verifier(t *testing.T, chain *appstoretest.Chain) *appstore.Verifier {
	t.Helper()
	v, err := appstore.New(appstore.WithRoot(chain.Root))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func reason(t *testing.T, err error) string {
	t.Helper()
	var r *appstore.Reject
	if !errors.As(err, &r) || !errors.Is(err, appstore.ErrInvalid) {
		t.Fatalf("want a *Reject, got %v", err)
	}
	return r.Reason
}

// Кіріктірілген түбір — дәл Apple Root CA - G3: саусақ ізі өзгерсе, тест (және сервер) құлайды.
func TestEmbeddedAppleRootIsPinned(t *testing.T) {
	root, err := appstore.AppleRoot()
	if err != nil {
		t.Fatalf("embedded root: %v", err)
	}
	sum := sha256.Sum256(root.Raw)
	if got := strings.ToUpper(hex.EncodeToString(sum[:])); got != "63343ABFB89A6A03EBB57E9B3F5FA7BE7C4F5C756F3017B3A8C488C3653E9179" {
		t.Fatalf("fingerprint %s", got)
	}
	if root.Subject.CommonName != "Apple Root CA - G3" || !root.IsCA {
		t.Fatalf("subject %v CA %v", root.Subject, root.IsCA)
	}
	if key, ok := root.PublicKey.(*ecdsa.PublicKey); !ok || key.Curve != elliptic.P384() {
		t.Fatal("Apple Root CA - G3 is a P-384 key")
	}
	if _, err := appstore.New(); err != nil {
		t.Fatalf("default verifier: %v", err)
	}
}

func TestValidTransactionVerifies(t *testing.T) {
	chain := appstoretest.NewChain(t, appstoretest.Options{})
	got, err := verifier(t, chain).Transaction(chain.Sign(t, transaction()))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	want := transaction()
	if got.TransactionID != want.TransactionID || got.ProductID != want.ProductID || got.BundleID != want.BundleID ||
		got.AppAccountToken != want.AppAccountToken || got.Type != appstore.TypeConsumable || got.Quantity != 1 ||
		got.Environment != "Production" || got.SignedDate != want.SignedDate || got.RevocationDate != 0 {
		t.Fatalf("payload %+v", got)
	}
}

// Apple белгілері critical болса да тізбек өтеді (белгілерді сервер өзі тексереді).
func TestCriticalAppleMarkersAreHandled(t *testing.T) {
	chain := appstoretest.NewChain(t, appstoretest.Options{CriticalOIDs: true})
	if _, err := verifier(t, chain).Transaction(chain.Sign(t, transaction())); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestForgedTransactionsAreRefused(t *testing.T) {
	chain := appstoretest.NewChain(t, appstoretest.Options{})
	other := appstoretest.NewChain(t, appstoretest.Options{})
	v := verifier(t, chain)
	valid := chain.Sign(t, transaction())
	parts := strings.Split(valid, ".")

	tampered := transaction()
	tampered.ProductID = "kz.aireply.pack1000"
	tamperedPayload := strings.Split(chain.Sign(t, tampered), ".")[1]

	// another chain whose x5c ends at the trusted root bytes, but the leaf was not issued by it
	mixed := other.SignWithHeader(t, map[string]any{"alg": "ES256", "x5c": []string{
		other.X5C()[0], other.X5C()[1], chain.X5C()[2]}}, transaction())

	cases := []struct {
		name, jws, reason string
	}{
		{"another root", other.Sign(t, transaction()), appstore.ReasonChain},
		{"leaf of another chain under the trusted root", mixed, appstore.ReasonChain},
		{"alg none", chain.SignWithHeader(t, map[string]any{"alg": "none", "x5c": chain.X5C()}, transaction()), appstore.ReasonAlgorithm},
		{"HS256", chain.SignWithHeader(t, map[string]any{"alg": "HS256", "x5c": chain.X5C()}, transaction()), appstore.ReasonAlgorithm},
		{"tampered payload", parts[0] + "." + tamperedPayload + "." + parts[2], appstore.ReasonSignature},
		{"signature of another key", parts[0] + "." + parts[1] + "." + strings.Split(other.Sign(t, transaction()), ".")[2], appstore.ReasonSignature},
		{"ASN.1 signature", parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 70)), appstore.ReasonSignature},
		{"two certificates", chain.SignWithHeader(t, map[string]any{"alg": "ES256", "x5c": chain.X5C()[:2]}, transaction()), appstore.ReasonChain},
		{"no x5c", chain.SignWithHeader(t, map[string]any{"alg": "ES256"}, transaction()), appstore.ReasonChain},
		{"not a JWS", "abc.def", appstore.ReasonMalformed},
		{"empty", "", appstore.ReasonMalformed},
		{"oversized", strings.Repeat("a", 70<<10), appstore.ReasonMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Transaction(tc.jws)
			if got := reason(t, err); got != tc.reason {
				t.Fatalf("reason %q, want %q (%v)", got, tc.reason, err)
			}
		})
	}
}

func TestChainWithoutAppleMarkersIsRefused(t *testing.T) {
	for name, opts := range map[string]appstoretest.Options{
		"leaf OID":         {NoLeafOID: true},
		"intermediate OID": {NoIntermediateOID: true},
	} {
		t.Run(name, func(t *testing.T) {
			chain := appstoretest.NewChain(t, opts)
			_, err := verifier(t, chain).Transaction(chain.Sign(t, transaction()))
			if got := reason(t, err); got != appstore.ReasonChain {
				t.Fatalf("reason %q (%v)", got, err)
			}
		})
	}
}

// Тізбек signedDate сәтінде жарамды болуы керек.
func TestChainMustBeValidAtSignedDate(t *testing.T) {
	chain := appstoretest.NewChain(t, appstoretest.Options{
		NotBefore: signedAt.Add(-48 * time.Hour), NotAfter: signedAt.Add(-24 * time.Hour)})
	_, err := verifier(t, chain).Transaction(chain.Sign(t, transaction()))
	if got := reason(t, err); got != appstore.ReasonChain {
		t.Fatalf("reason %q (%v)", got, err)
	}
	noDate := transaction()
	noDate.SignedDate = 0
	valid := appstoretest.NewChain(t, appstoretest.Options{})
	if got := reason(t, func() error { _, err := verifier(t, valid).Transaction(valid.Sign(t, noDate)); return err }()); got != appstore.ReasonMalformed {
		t.Fatalf("no signedDate: %q", got)
	}
}

// Кіріктірілген Apple түбірі жалған тізбекке сенбейді.
func TestDefaultVerifierTrustsOnlyApple(t *testing.T) {
	v, err := appstore.New()
	if err != nil {
		t.Fatal(err)
	}
	chain := appstoretest.NewChain(t, appstoretest.Options{})
	if got := reason(t, func() error { _, err := v.Transaction(chain.Sign(t, transaction())); return err }()); got != appstore.ReasonChain {
		t.Fatalf("reason %q", got)
	}
}

func TestNotificationVerifies(t *testing.T) {
	chain := appstoretest.NewChain(t, appstoretest.Options{})
	signedTx := chain.Sign(t, transaction())
	payload := appstoretest.Notification{
		NotificationType: appstore.NotificationRefund, NotificationUUID: "3b8c3d55-1a4d-4bd1-9b1e-0c6f2a7f0a11",
		Version: "2.0", SignedDate: signedAt.UnixMilli(),
		Data: appstoretest.NotificationData{BundleID: "kz.ai-reply.reply.keyboard.keyboard", Environment: "Production",
			SignedTransactionInfo: signedTx},
	}
	n, err := verifier(t, chain).Notification(chain.Sign(t, payload))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if n.NotificationType != "REFUND" || n.Data.SignedTransactionInfo != signedTx || n.NotificationUUID != payload.NotificationUUID {
		t.Fatalf("notification %+v", n)
	}
	payload.NotificationUUID = ""
	if got := reason(t, func() error { _, err := verifier(t, chain).Notification(chain.Sign(t, payload)); return err }()); got != appstore.ReasonMalformed {
		t.Fatalf("no uuid: %q", got)
	}
}
