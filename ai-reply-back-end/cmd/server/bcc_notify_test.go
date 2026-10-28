package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc/sandboxnotify"
)

func notifyMainConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		App:      config.App{PublicBaseURL: "https://ai-reply.kz"},
		Database: config.Database{Path: filepath.Join(t.TempDir(), "aireply.db")},
		BCCSandboxNotify: config.BCCSandboxNotify{
			Enabled:         true,
			PublicURL:       "https://ai-reply.kz:443" + sandboxnotify.NotifyPath,
			ManagementToken: base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("m", 32))),
		},
	}
}

func callBCCMain(mux http.Handler, method, path, body, contentType, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestBCCNotifyDisabledCreatesNeitherRouteNorDatabase(t *testing.T) {
	cfg := notifyMainConfig(t)
	cfg.BCCSandboxNotify.Enabled = false
	mux := http.NewServeMux()
	closer, err := registerBCCSandboxNotify(mux, cfg)
	if err != nil || closer != nil {
		t.Fatalf("disabled receiver opened: %v", err)
	}
	if w := callBCCMain(mux, "GET", sandboxnotify.NotifyPath+"/healthz", "", "", ""); w.Code != 404 {
		t.Fatal("disabled receiver route registered")
	}
	entries, err := os.ReadDir(filepath.Dir(cfg.Database.Path))
	if err != nil || len(entries) != 0 {
		t.Fatal("disabled receiver created database")
	}
}

func TestBCCNotifyOnExistingMuxRegistersAndPersistsCallback(t *testing.T) {
	cfg := notifyMainConfig(t)
	if err := os.WriteFile(cfg.Database.Path, []byte("application database sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	closer, err := registerBCCSandboxNotify(mux, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	if w := callBCCMain(mux, "GET", "/healthz", "", "", ""); w.Code != 204 {
		t.Fatal("existing backend route changed")
	}
	if w := callBCCMain(mux, "GET", sandboxnotify.NotifyPath+"/healthz", "", "", ""); w.Code != 200 {
		t.Fatalf("receiver health: %d %s", w.Code, w.Body.String())
	}
	fields := map[string]string{
		"ORDER": "1793181157379482539", "TERMINAL": "88888881", "MERCHANT": "00000001", "TRTYPE": "1", "AMOUNT": "100.00", "CURRENCY": "398",
		"NOTIFY_URL": cfg.BCCSandboxNotify.PublicURL, "NONCE": strings.Repeat("B", 32), "P_SIGN": strings.Repeat("A", 40),
		"MERCH_RN_ID": "0123456789ABCDEF", "MERCH_TRAN_STATE": "S",
	}
	body, _ := json.Marshal(fields)
	if w := callBCCMain(mux, "POST", sandboxnotify.ManagementPrefix+"register", string(body), "application/json", ""); w.Code != 401 {
		t.Fatal("registration accepts unauthenticated request")
	}
	if w := callBCCMain(mux, "POST", sandboxnotify.ManagementPrefix+"register", string(body), "application/json", cfg.BCCSandboxNotify.ManagementToken); w.Code != 201 {
		t.Fatalf("registration: %d %s", w.Code, w.Body.String())
	}
	values := url.Values{}
	for key, value := range fields {
		values.Set(key, value)
	}
	values.Set("ACTION", "0")
	values.Set("RC", "00")
	values.Set("RRN", "630123135061")
	values.Set("INT_REF", "2E1D94FE4E05D190")
	values.Set("CVC2_RC", "")
	for i := 0; i < 2; i++ {
		if w := callBCCMain(mux, "POST", sandboxnotify.NotifyPath, values.Encode(), "application/x-www-form-urlencoded", ""); w.Code != 200 {
			t.Fatalf("callback: %d %s", w.Code, w.Body.String())
		}
	}
	response := callBCCMain(mux, "GET", sandboxnotify.ManagementPrefix+"events?order="+fields["ORDER"], "", "", cfg.BCCSandboxNotify.ManagementToken)
	var result struct {
		Records []sandboxnotify.Record `json:"records"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Records) != 1 || result.Records[0].State != "received_unverified" || result.Records[0].RawBody != values.Encode() {
		t.Fatalf("persisted callback: %d %s", response.Code, response.Body.String())
	}
	actual, err := os.ReadFile(cfg.Database.Path)
	if err != nil || string(actual) != "application database sentinel" {
		t.Fatal("application database was modified")
	}
}

func TestBCCNotifyRefusesApplicationDatabaseCollision(t *testing.T) {
	cfg := notifyMainConfig(t)
	cfg.Database.Path = filepath.Join(filepath.Dir(cfg.Database.Path), "bcc-sandbox-notify.sqlite")
	if closer, err := registerBCCSandboxNotify(http.NewServeMux(), cfg); err == nil || closer != nil {
		t.Fatal("shared application database accepted")
	}
}
