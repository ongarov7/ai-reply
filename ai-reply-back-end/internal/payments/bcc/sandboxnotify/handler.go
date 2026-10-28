package sandboxnotify

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const NotifyPath = "/api/v1/payments/bcc/sandbox/notify"
const ManagementPrefix = "/api/v1/payments/bcc/sandbox/manage/"

type Config struct {
	Enabled           bool
	PublicURL         string
	Listen            string
	BasicUsername     string
	BasicPassword     string
	ContainerListener bool
	ManagementToken   string
}

// Validate requires an explicitly enabled sandbox collector with a stable
// public callback URL and a separate secret for its management operations.
func (c Config) Validate() error {
	if !c.Enabled {
		return errors.New("sandbox notify: explicitly enable the sandbox receiver")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Port() != "443" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != NotifyPath {
		return ErrInvalid
	}
	if u.Hostname() == "localhost" || strings.HasSuffix(u.Hostname(), ".localhost") || strings.HasSuffix(u.Hostname(), ".local") {
		return ErrInvalid
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || !ip.IsGlobalUnicast()) {
		return ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(c.ManagementToken)
	if err != nil || len(decoded) != 32 || len(c.ManagementToken) != 43 {
		return ErrInvalid
	}
	if (c.BasicUsername == "") != (c.BasicPassword == "") || strings.Contains(c.BasicUsername, ":") {
		return ErrInvalid
	}
	if c.BasicUsername != "" && (len(c.BasicUsername) > 128 || len(c.BasicPassword) < 32 || len(c.BasicPassword) > 256) {
		return ErrInvalid
	}
	if c.Listen != "" {
		host, port, err := net.SplitHostPort(c.Listen)
		portNumber, portErr := strconv.Atoi(port)
		ip := net.ParseIP(host)
		if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 || ip == nil || !ip.IsLoopback() && !(c.ContainerListener && ip.IsUnspecified()) {
			return errors.New("sandbox notify: listener must be loopback or an explicitly enabled private container listener")
		}
	}
	return nil
}

type Handler struct {
	store          *Store
	pathHash       [32]byte
	healthPathHash [32]byte
	basicUserHash  [32]byte
	basicPassHash  [32]byte
	basic          bool
	management     bool
	managementHash [32]byte
	publicURL      string
}

func NewHandler(store *Store, cfg Config) (*Handler, error) {
	if store == nil {
		return nil, ErrInvalid
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	u, _ := url.Parse(cfg.PublicURL)
	return &Handler{store: store, pathHash: sha256.Sum256([]byte(u.Path)), healthPathHash: sha256.Sum256([]byte(u.Path + "/healthz")), basicUserHash: sha256.Sum256([]byte(cfg.BasicUsername)), basicPassHash: sha256.Sum256([]byte(cfg.BasicPassword)), basic: cfg.BasicUsername != "", management: cfg.ManagementToken != "", managementHash: sha256.Sum256([]byte(cfg.ManagementToken)), publicURL: cfg.PublicURL}, nil
}

// ServeHTTP never logs Authorization or notification bodies. Received fields
// remain unverified payment evidence even after a registered-request match.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(r.URL.Path, ManagementPrefix) {
		h.serveManagement(w, r)
		return
	}
	path := sha256.Sum256([]byte(r.URL.Path))
	if subtle.ConstantTimeCompare(path[:], h.healthPathHash[:]) == 1 && r.URL.RawPath == "" && r.URL.RawQuery == "" && r.Method == http.MethodGet {
		if err := h.store.db.Reader().PingContext(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true,\"mode\":\"sandbox_capture_only\"}\n")
		return
	}
	if subtle.ConstantTimeCompare(path[:], h.pathHash[:]) != 1 || r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth := "public_endpoint"
	if h.basic {
		user, pass, ok := r.BasicAuth()
		uh, ph := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(pass))
		if !ok || subtle.ConstantTimeCompare(uh[:], h.basicUserHash[:]) != 1 || subtle.ConstantTimeCompare(ph[:], h.basicPassHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="bcc-sandbox", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		auth = "http_basic"
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "query parameters are not accepted", http.StatusBadRequest)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request", http.StatusBadRequest)
		}
		return
	}
	fields, err := ParseForm(string(raw))
	if err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_, err = h.store.Record(r.Context(), fields, string(raw), auth)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknown):
			http.Error(w, "unregistered test order", http.StatusNotFound)
		case errors.Is(err, ErrInvalid), errors.Is(err, ErrMismatch):
			http.Error(w, "test fields rejected", http.StatusBadRequest)
		default:
			http.Error(w, "notification not saved", http.StatusServiceUnavailable)
		}
		return
	}
	// 200/OK after durable commit is this collector's ACK policy, not a claim
	// that BCC has confirmed its notification retry/acknowledgment contract.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "OK\n")
}

func (h *Handler) serveManagement(w http.ResponseWriter, r *http.Request) {
	if !h.management || r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	values := r.Header.Values("Authorization")
	token := ""
	if len(values) == 1 && strings.HasPrefix(values[0], "Bearer ") {
		token = strings.TrimPrefix(values[0], "Bearer ")
	}
	hash := sha256.Sum256([]byte(token))
	if token == "" || subtle.ConstantTimeCompare(hash[:], h.managementHash[:]) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case ManagementPrefix + "register":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "query parameters are not accepted", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var max *http.MaxBytesError
			if errors.As(err, &max) {
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "invalid request", http.StatusBadRequest)
			}
			return
		}
		fields, err := ParseJSONFields(body)
		if err != nil {
			http.Error(w, "invalid request fields", http.StatusBadRequest)
			return
		}
		err = h.store.Register(r.Context(), fields, h.publicURL)
		if err != nil {
			if errors.Is(err, ErrInvalid) || errors.Is(err, ErrMismatch) {
				http.Error(w, "test fields rejected", http.StatusBadRequest)
			} else {
				http.Error(w, "test not registered", http.StatusServiceUnavailable)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "{\"registered\":true}\n")
	case ManagementPrefix + "events":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["order"]) != 1 || !orderOK(query.Get("order")) {
			http.Error(w, "one test order is required", http.StatusBadRequest)
			return
		}
		records, err := h.store.Records(r.Context(), query.Get("order"))
		if err != nil {
			http.Error(w, "events unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Mode    string   `json:"mode"`
			Records []Record `json:"records"`
		}{"sandbox_capture_only", records})
	default:
		http.NotFound(w, r)
	}
}

// ParseJSONFields accepts only one string-valued object, preserving empty
// fields and rejecting duplicate names, nesting and trailing JSON.
func ParseJSONFields(body []byte) (map[string]string, error) {
	if len(body) > MaxBodyBytes {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrInvalid
	}
	fields := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		k, ok := key.(string)
		if !ok {
			return nil, ErrInvalid
		}
		if _, exists := fields[k]; exists {
			return nil, ErrInvalid
		}
		value, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		v, ok := value.(string)
		if !ok {
			return nil, ErrInvalid
		}
		fields[k] = v
		if len(fields) > 128 {
			return nil, ErrInvalid
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	if err := validFields(fields); err != nil {
		return nil, err
	}
	return fields, nil
}
