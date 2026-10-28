package sandboxnotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testConfig() Config {
	return Config{Enabled: true, PublicURL: "https://merchant.example:443" + NotifyPath, Listen: "127.0.0.1:8187", ManagementToken: base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("m", 32)))}
}
func testFields(cfg Config) map[string]string {
	return map[string]string{"ORDER": "1793181157379482539", "TERMINAL": "88888881", "MERCHANT": "00000001", "TRTYPE": "1", "AMOUNT": "100.00", "CURRENCY": "398", "NOTIFY_URL": cfg.PublicURL, "NONCE": strings.Repeat("B", 32), "P_SIGN": strings.Repeat("A", 40), "MERCH_RN_ID": "0123456789ABCDEF", "MERCH_TRAN_STATE": "S", "M_INFO": "dGVzdA==", "NAME": "NAME SURNAME", "EMAIL": "cardholder@example.invalid"}
}
func copyFields(f map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range f {
		out[k] = v
	}
	return out
}
func notification(f map[string]string) map[string]string {
	out := copyFields(f)
	out["ACTION"] = "0"
	out["RC"] = "00"
	out["RRN"] = "630123135061"
	out["INT_REF"] = "2E1D94FE4E05D190"
	out["CVC2_RC"] = ""
	return out
}
func encode(f map[string]string) string {
	v := url.Values{}
	for k, s := range f {
		v.Set(k, s)
	}
	return v.Encode()
}
func request(h http.Handler, method, path, content, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if content != "" {
		r.Header.Set("Content-Type", content)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "notify.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestManagementRegistrationCallbackDurability(t *testing.T) {
	cfg := testConfig()
	dbfile := filepath.Join(t.TempDir(), "notify.sqlite")
	store, err := Open(dbfile)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	fields := testFields(cfg)
	body, _ := json.Marshal(fields)
	if w := request(h, "POST", ManagementPrefix+"register", "application/json", string(body), "wrong"); w.Code != 401 {
		t.Fatalf("wrong bearer accepted: %d", w.Code)
	}
	if w := request(h, "POST", ManagementPrefix+"register", "application/json", string(body), cfg.ManagementToken); w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	u, _ := url.Parse(cfg.PublicURL)
	actual := notification(fields)
	raw := encode(actual)
	for i := 0; i < 2; i++ {
		if w := request(h, "POST", u.Path, "application/x-www-form-urlencoded", raw, ""); w.Code != 200 {
			t.Fatalf("callback: %d %s", w.Code, w.Body.String())
		}
	}
	changed := copyFields(fields)
	changed["AMOUNT"] = "101.00"
	bad, _ := json.Marshal(changed)
	if w := request(h, "POST", ManagementPrefix+"register", "application/json", string(bad), cfg.ManagementToken); w.Code != 400 {
		t.Fatal("changed order accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dbfile)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.Records(context.Background(), fields["ORDER"])
	if err != nil || len(records) != 1 {
		t.Fatalf("not durable/deduplicated: %v %d", err, len(records))
	}
	if records[0].State != "received_unverified" || records[0].RawBody != raw || records[0].Fields["EMAIL"] != fields["EMAIL"] {
		t.Fatal("evidence changed or promoted to payment success")
	}
	if value, exists := records[0].Fields["CVC2_RC"]; !exists || value != "" {
		t.Fatal("empty received field lost")
	}
	h, err = NewHandler(reopened, cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := request(h, "GET", ManagementPrefix+"events?order="+fields["ORDER"], "", "", cfg.ManagementToken)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"received_unverified"`) {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", ManagementPrefix+"events?order=999999", "", "", cfg.ManagementToken)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"records":[]`) {
		t.Fatalf("empty export must be []: %s", w.Body.String())
	}
}

func TestNotifyRejectsUnknownAmbiguousAndSensitiveData(t *testing.T) {
	cfg := testConfig()
	s := newTestStore(t)
	f := testFields(cfg)
	if err := s.Register(context.Background(), f, cfg.PublicURL); err != nil {
		t.Fatal(err)
	}
	h, _ := NewHandler(s, cfg)
	u, _ := url.Parse(cfg.PublicURL)
	for _, tc := range []struct {
		name, method, path, content, body string
		want                              int
	}{
		{"query", "POST", u.Path + "?ORDER=123456", "application/x-www-form-urlencoded", encode(notification(f)), 400},
		{"duplicate", "POST", u.Path, "application/x-www-form-urlencoded", encode(notification(f)) + "&ORDER=" + f["ORDER"], 400},
		{"card data", "POST", u.Path, "application/x-www-form-urlencoded", encode(notification(f)) + "&PAN=4463755556467828", 400},
		{"size", "POST", u.Path, "application/x-www-form-urlencoded", "x=" + strings.Repeat("x", MaxBodyBytes), 413},
		{"json", "POST", u.Path, "application/json", "{}", 415},
		{"get", "GET", u.Path, "", "", 405},
		{"wrong path", "POST", u.Path + "x", "application/x-www-form-urlencoded", encode(notification(f)), 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(h, tc.method, tc.path, tc.content, tc.body, "")
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
	for _, key := range []string{"ORDER", "TERMINAL", "TRTYPE", "CURRENCY", "AMOUNT", "NONCE", "P_SIGN", "MERCH_RN_ID", "MERCH_TRAN_STATE"} {
		t.Run("mismatch "+key, func(t *testing.T) {
			got := notification(f)
			got[key] = map[string]string{"ORDER": "999999", "TERMINAL": "99999999", "TRTYPE": "171", "CURRENCY": "840", "AMOUNT": "99.00", "NONCE": strings.Repeat("C", 32), "P_SIGN": strings.Repeat("D", 40), "MERCH_RN_ID": "FEDCBA9876543210", "MERCH_TRAN_STATE": "M"}[key]
			w := request(h, "POST", u.Path, "application/x-www-form-urlencoded", encode(got), "")
			if w.Code != 400 && w.Code != 404 {
				t.Fatalf("mismatch accepted: %d", w.Code)
			}
		})
	}
	records, err := s.Records(context.Background(), "")
	if err != nil || len(records) != 0 {
		t.Fatal("rejected callback retained")
	}
}

func TestConcurrentDeliveryAndSharedOrderAdjustment(t *testing.T) {
	cfg := testConfig()
	s := newTestStore(t)
	f := testFields(cfg)
	if err := s.Register(context.Background(), f, cfg.PublicURL); err != nil {
		t.Fatal(err)
	}
	adjust := copyFields(f)
	adjust["TRTYPE"] = "22"
	delete(adjust, "MERCHANT")
	if err := s.Register(context.Background(), adjust, cfg.PublicURL); err != nil {
		t.Fatal("same ORDER different type rejected", err)
	}
	h, _ := NewHandler(s, cfg)
	u, _ := url.Parse(cfg.PublicURL)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(h, "POST", u.Path, "application/x-www-form-urlencoded", encode(notification(f)), "")
			if w.Code != 200 {
				t.Errorf("concurrent delivery: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	w := request(h, "POST", u.Path, "application/x-www-form-urlencoded", encode(notification(adjust)), "")
	if w.Code != 200 {
		t.Fatalf("adjustment callback rejected %d %s", w.Code, w.Body.String())
	}
	records, err := s.Records(context.Background(), f["ORDER"])
	if err != nil || len(records) != 2 {
		t.Fatalf("expected two operation types: %v %d", err, len(records))
	}
}

func TestManagementRequiredAndBasicAuth(t *testing.T) {
	cfg := testConfig()
	missing := cfg
	missing.ManagementToken = ""
	s := newTestStore(t)
	if _, err := NewHandler(s, missing); err == nil {
		t.Fatal("enabled collector accepted missing management secret")
	}
	cfg.BasicUsername = "bcc-test"
	cfg.BasicPassword = strings.Repeat("p", 32)
	h, err := NewHandler(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(h, "POST", ManagementPrefix+"register", "application/json", "{}", "guess"); w.Code != 401 {
		t.Fatal("management accepted incorrect token")
	}
	f := testFields(cfg)
	if err := s.Register(context.Background(), f, cfg.PublicURL); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(cfg.PublicURL)
	if w := request(h, "POST", u.Path, "application/x-www-form-urlencoded", encode(notification(f)), ""); w.Code != 401 {
		t.Fatal("missing basic auth accepted")
	}
	r := httptest.NewRequest("POST", u.Path, strings.NewReader(encode(notification(f))))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(cfg.BasicUsername, cfg.BasicPassword)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("valid basic rejected: %d", w.Code)
	}
	records, _ := s.Records(context.Background(), f["ORDER"])
	if records[0].TransportAuth != "http_basic" {
		t.Fatal("transport auth not distinguished")
	}
}

func TestConfigurationAndRegistrationLimits(t *testing.T) {
	good := testConfig()
	for name, patch := range map[string]func(*Config){"off": func(c *Config) { c.Enabled = false }, "http": func(c *Config) { c.PublicURL = strings.Replace(c.PublicURL, "https:", "http:", 1) }, "no port": func(c *Config) { c.PublicURL = strings.Replace(c.PublicURL, ":443", "", 1) }, "private host": func(c *Config) { c.PublicURL = strings.Replace(c.PublicURL, "merchant.example", "127.0.0.1", 1) }, "userinfo": func(c *Config) {
		c.PublicURL = strings.Replace(c.PublicURL, "merchant.example", "user@merchant.example", 1)
	}, "query": func(c *Config) { c.PublicURL += "?x=1" }, "public bind": func(c *Config) { c.Listen = "0.0.0.0:8080" }, "weak token": func(c *Config) { c.ManagementToken = "weak" }, "legacy secret url": func(c *Config) {
		c.PublicURL += "/" + strings.Repeat("c", 43)
	}} {
		t.Run(name, func(t *testing.T) {
			c := good
			patch(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
	container := good
	container.Listen = "0.0.0.0:8080"
	container.ContainerListener = true
	if err := container.Validate(); err != nil {
		t.Fatal(err)
	}
	embedded := good
	embedded.Listen = ""
	if err := embedded.Validate(); err != nil {
		t.Fatal("embedded handler should not require own listener", err)
	}
	for _, raw := range []string{`{"ORDER":"1","ORDER":"2"}`, `{"ORDER":1}`, `{"ORDER":{"x":"y"}}`, `{} {}`, `{"PAN":"123"}`} {
		if _, err := ParseJSONFields([]byte(raw)); err == nil {
			t.Fatalf("ambiguous JSON accepted: %s", raw)
		}
	}
	s := newTestStore(t)
	f := testFields(good)
	if err := s.Register(context.Background(), f, good.PublicURL); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		n := notification(f)
		n["RC_TEXT"] = fmt.Sprint(i)
		if _, err := s.Record(context.Background(), n, encode(n), "public_endpoint"); err != nil {
			t.Fatal(err)
		}
	}
	n := notification(f)
	n["RC_TEXT"] = "seventeenth"
	if _, err := s.Record(context.Background(), n, encode(n), "public_endpoint"); err == nil {
		t.Fatal("unbounded event history accepted")
	}
}

func TestFullSyncSurvivesWriterConnectionRecycle(t *testing.T) {
	s := newTestStore(t)
	var level int
	if err := s.db.Writer().QueryRow(`PRAGMA synchronous`).Scan(&level); err != nil || level != 2 {
		t.Fatalf("FULL synchronous required before recycle: %d %v", level, err)
	}
	// With no idle connections each query obtains a newly opened connection.
	s.db.Writer().SetMaxIdleConns(0)
	if err := s.db.Writer().QueryRow(`PRAGMA synchronous`).Scan(&level); err != nil || level != 2 {
		t.Fatalf("FULL synchronous lost after recycle: %d %v", level, err)
	}
}
