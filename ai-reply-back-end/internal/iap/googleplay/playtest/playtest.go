// Package playtest — тесттерге арналған жалған Google OAuth және androidpublisher v3 сервері.
//
// The token endpoint checks the service-account assertion (RS256, the
// androidpublisher scope, aud = its own URL); the purchase endpoints serve the
// purchases a test put there and change their state the way Google does:
// consume sets consumptionState 1, the voided list returns what the test voided.
package playtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
)

// ClientEmail — жалған қызметтік тіркелгі.
const ClientEmail = "play@ai-reply-test.iam.gserviceaccount.com"

const accessToken = "ya29.play-test-access"

var (
	keyOnce sync.Once
	key     *rsa.PrivateKey
	keyPEM  string
)

// Key — бір процесске бір RSA кілті (тесттер жылдам болсын).
func Key(t testing.TB) (*rsa.PrivateKey, string) {
	t.Helper()
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			panic(err)
		}
		key, keyPEM = k, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	})
	return key, keyPEM
}

// Server — жалған Google.
type Server struct {
	*httptest.Server
	Package string

	TokenCalls, GetCalls, ConsumeCalls, VoidedCalls atomic.Int32

	mu        sync.Mutex
	purchases map[string]*googleplay.ProductPurchase
	voided    []googleplay.VoidedPurchase
	failNext  int // the next n purchase calls answer 503
	failEat   int // the next n consume calls answer 503
	pageSize  int
	lastSince string
}

// New — сервер; тест біткенде жабылады.
func New(t testing.TB, pkg string) *Server {
	t.Helper()
	pub, _ := Key(t)
	s := &Server{Package: pkg, purchases: map[string]*googleplay.ProductPurchase{}, pageSize: 1000}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.handle(t, &pub.PublicKey, w, r) }))
	t.Cleanup(s.Close)
	return s
}

// Config — клиент баптауы (осы серверге бағытталған).
func (s *Server) Config(t testing.TB) googleplay.Config {
	_, pemKey := Key(t)
	return googleplay.Config{PackageName: s.Package, ClientEmail: ClientEmail, PrivateKey: pemKey,
		BaseURL: s.URL, TokenURL: s.URL + "/token", HTTPClient: s.Client()}
}

func purchaseKey(productID, token string) string { return productID + "\x00" + token }

// Put — Google білетін сатып алу.
func (s *Server) Put(productID, token string, p googleplay.ProductPurchase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := p
	s.purchases[purchaseKey(productID, token)] = &stored
}

// Purchase — сатып алудың қазіргі күйі.
func (s *Server) Purchase(productID, token string) googleplay.ProductPurchase {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.purchases[purchaseKey(productID, token)]; p != nil {
		return *p
	}
	return googleplay.ProductPurchase{}
}

// Update — сатып алу күйін өзгерту (pending → purchased).
func (s *Server) Update(productID, token string, change func(p *googleplay.ProductPurchase)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.purchases[purchaseKey(productID, token)]; p != nil {
		change(p)
	}
}

// FailNext — келесі n сатып алу сұранысы 503 қайтарады.
func (s *Server) FailNext(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = n
}

// FailConsumes — келесі n тұтыну сұранысы 503 қайтарады (тексеру сәтті өтеді).
func (s *Server) FailConsumes(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failEat = n
}

// Void — қайтарылған сатып алу (voidedpurchases тізімінде).
func (s *Server) Void(v googleplay.VoidedPurchase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voided = append(s.voided, v)
}

// PageSize — voidedpurchases бетінің өлшемі (беттеуді тексеру үшін).
func (s *Server) PageSize(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageSize = n
}

// LastVoidedSince — соңғы voidedpurchases сұранысының startTime мәні.
func (s *Server) LastVoidedSince() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSince
}

func (s *Server) handle(t testing.TB, pub *rsa.PublicKey, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		s.TokenCalls.Add(1)
		_ = r.ParseForm()
		if !s.validAssertion(pub, r.Form.Get("assertion")) || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","expires_in":3599,"token_type":"Bearer"}`)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+accessToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
		return
	}
	prefix := "/androidpublisher/v3/applications/" + s.Package + "/purchases/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	if rest == "voidedpurchases" && r.Method == http.MethodGet {
		s.VoidedCalls.Add(1)
		s.voidedPage(w, r)
		return
	}
	s.mu.Lock()
	fail := s.failNext > 0
	if fail {
		s.failNext--
	}
	s.mu.Unlock()
	if fail {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"code":503,"status":"UNAVAILABLE","errors":[{"reason":"backendError"}]}}`)
		return
	}
	// products/{productId}/tokens/{token}[:consume]
	parts := strings.Split(strings.TrimPrefix(rest, "products/"), "/tokens/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	productID, _ := url.PathUnescape(parts[0])
	token, consume := parts[1], false
	if strings.HasSuffix(token, ":consume") {
		token, consume = strings.TrimSuffix(token, ":consume"), true
	}
	token, _ = url.PathUnescape(token)

	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.purchases[purchaseKey(productID, token)]
	if p == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":404,"status":"NOT_FOUND","errors":[{"reason":"purchaseTokenNotFound"}]}}`)
		return
	}
	switch {
	case consume && r.Method == http.MethodPost:
		s.ConsumeCalls.Add(1)
		if s.failEat > 0 {
			s.failEat--
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":503,"status":"UNAVAILABLE","errors":[{"reason":"backendError"}]}}`)
			return
		}
		if p.PurchaseState != googleplay.StatePurchased {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":400,"status":"FAILED_PRECONDITION","errors":[{"reason":"productNotOwnedByUser"}]}}`)
			return
		}
		p.ConsumptionState, p.AcknowledgementState = googleplay.Consumed, 1
		w.WriteHeader(http.StatusNoContent)
	case !consume && r.Method == http.MethodGet:
		s.GetCalls.Add(1)
		out := *p
		raw, _ := json.Marshal(out)
		_, _ = w.Write(raw)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) voidedPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSince = r.URL.Query().Get("startTime")
	start := 0
	if token := r.URL.Query().Get("token"); token != "" {
		for i, v := range s.voided {
			if v.PurchaseToken == token {
				start = i
			}
		}
	}
	end := min(start+s.pageSize, len(s.voided))
	page := map[string]any{"voidedPurchases": s.voided[start:end]}
	if end < len(s.voided) {
		page["tokenPagination"] = map[string]string{"nextPageToken": s.voided[end].PurchaseToken}
	}
	raw, _ := json.Marshal(page)
	_, _ = w.Write(raw)
}

func (s *Server) validAssertion(pub *rsa.PublicKey, assertion string) bool {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return false
	}
	decode := func(v string) []byte { b, _ := base64.RawURLEncoding.DecodeString(v); return b }
	var header, claims map[string]any
	if json.Unmarshal(decode(parts[0]), &header) != nil || json.Unmarshal(decode(parts[1]), &claims) != nil {
		return false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if header["alg"] != "RS256" || rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], decode(parts[2])) != nil {
		return false
	}
	return claims["iss"] == ClientEmail && claims["scope"] == googleplay.Scope && claims["aud"] == s.URL+"/token"
}
