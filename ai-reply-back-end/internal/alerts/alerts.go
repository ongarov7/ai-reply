// Package alerts — Telegram-ға өндірістік ескертулер.
//
// One lightweight channel for everything an operator must hear about:
// panics, failed payment paths, database and provider errors, downtime and
// recovery. Delivery is asynchronous (a bounded queue and one goroutine),
// identical alerts within a window are counted instead of re-sent, a token
// bucket caps the flow, and nothing here ever blocks or fails a request:
// when Telegram is down or the queue is full the alert is dropped and the
// drop is logged. The bot token stays in this process; values that look like
// secrets, e-mail addresses or long numbers never reach a message.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/logging"
)

// Severity — хабарлама деңгейі.
type Severity string

const (
	Warning   Severity = "WARNING"
	Error     Severity = "ERROR"
	Critical  Severity = "CRITICAL"
	Recovered Severity = "RECOVERED"
)

func (s Severity) icon() string {
	switch s {
	case Warning:
		return "⚠️"
	case Critical:
		return "🔴"
	case Recovered:
		return "✅"
	default:
		return "❌"
	}
}

// Config — Telegram баптауы және шектеулер.
type Config struct {
	Enabled  bool
	BotToken string
	ChatID   string
	// Service and Environment label every message ("ai-reply", "production").
	Service     string
	Environment string
	// DedupWindow — identical alerts inside the window are counted, not re-sent (default 5m).
	DedupWindow time.Duration
	// RatePerMinute — token bucket for sent messages (default 20); the rest are counted and summarised.
	RatePerMinute int
	// QueueSize — pending messages; a full queue drops the alert (default 256).
	QueueSize int
	// APIBase — Telegram Bot API origin; tests point it at a local server.
	APIBase string
	// Client — HTTP client; default has a 10-second timeout.
	Client *http.Client
}

const (
	defaultDedupWindow   = 5 * time.Minute
	defaultRatePerMinute = 20
	defaultQueueSize     = 256
	defaultAPIBase       = "https://api.telegram.org"
	maxTextRunes         = 3800 // Telegram allows 4096
	maxValueRunes        = 240
	maxFields            = 12
)

type field struct{ key, value string }

type message struct {
	severity Severity
	title    string
	fields   []field
	at       time.Time
	repeats  int
}

type occurrence struct {
	first time.Time
	count int
}

// Notifier — кезек, қайталануды басу және шектеу. Nil не өшірілген Notifier — бос операция.
type Notifier struct {
	cfg    Config
	log    *slog.Logger
	client *http.Client
	queue  chan message
	host   string
	now    func() time.Time

	mu         sync.Mutex
	seen       map[string]*occurrence
	tokens     float64
	refilledAt time.Time
	suppressed int // rate-limited or dropped since the last summary
	lastSweep  time.Time
}

// New — Notifier құрады. Өшірілген баптауда барлық әдіс бос операция.
//
// log must be the plain logger, never one wrapped by Handler: the notifier
// reports its own delivery problems there and must not alert about them.
func New(cfg Config, log *slog.Logger) *Notifier {
	if cfg.DedupWindow <= 0 {
		cfg.DedupWindow = defaultDedupWindow
	}
	if cfg.RatePerMinute <= 0 {
		cfg.RatePerMinute = defaultRatePerMinute
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultQueueSize
	}
	if cfg.APIBase == "" {
		cfg.APIBase = defaultAPIBase
	}
	if cfg.Service == "" {
		cfg.Service = "ai-reply"
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	n := &Notifier{cfg: cfg, log: log, now: time.Now}
	if !cfg.Enabled {
		return n
	}
	n.client = cfg.Client
	if n.client == nil {
		n.client = &http.Client{Timeout: 10 * time.Second}
	}
	n.queue = make(chan message, cfg.QueueSize)
	n.seen = map[string]*occurrence{}
	n.tokens = float64(cfg.RatePerMinute)
	n.refilledAt = n.now()
	n.lastSweep = n.refilledAt
	if host, err := os.Hostname(); err == nil {
		n.host = host
	}
	return n
}

// Enabled — хабарламалар жіберіле ме.
func (n *Notifier) Enabled() bool { return n != nil && n.cfg.Enabled }

// Notify — ескерту кезекке қойылады; ешқашан бұғаттамайды.
//
// kv are key/value pairs like slog's: forbidden keys (see logging) are
// dropped, values that look like secrets are redacted and long values cut.
func (n *Notifier) Notify(severity Severity, title string, kv ...any) {
	if !n.Enabled() {
		return
	}
	n.enqueue(message{severity: severity, title: title, fields: fieldsFromKV(kv)})
}

func (n *Notifier) enqueue(m message) {
	m.title = strings.TrimSpace(m.title)
	if m.title == "" {
		m.title = "(no title)"
	}
	m.at = n.now()
	if !n.admit(&m) {
		return
	}
	select {
	case n.queue <- m:
	default:
		n.mu.Lock()
		n.suppressed++
		n.mu.Unlock()
	}
}

// admit — қайталануды санайды және шелек токенін алады. true болса жіберуге болады.
func (n *Notifier) admit(m *message) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := m.at
	key := m.identity()
	if occ := n.seen[key]; occ != nil {
		if now.Sub(occ.first) < n.cfg.DedupWindow {
			occ.count++
			return false
		}
		m.repeats = occ.count
		occ.first, occ.count = now, 0
	} else {
		n.seen[key] = &occurrence{first: now}
	}
	if now.Sub(n.lastSweep) > n.cfg.DedupWindow || len(n.seen) > 512 {
		for k, occ := range n.seen {
			if now.Sub(occ.first) >= n.cfg.DedupWindow && occ.count == 0 {
				delete(n.seen, k)
			}
		}
		n.lastSweep = now
	}
	// Token bucket: RatePerMinute burst, refilled continuously.
	rate := float64(n.cfg.RatePerMinute)
	if elapsed := now.Sub(n.refilledAt); elapsed > 0 {
		n.tokens += elapsed.Minutes() * rate
	}
	if n.tokens > rate {
		n.tokens = rate
	}
	n.refilledAt = now
	if n.tokens < 1 {
		n.suppressed++
		return false
	}
	n.tokens--
	return true
}

// identityKeys — бір оқиғаны анықтайтын өрістер (request_id, user_id және т.б. емес).
var identityKeys = map[string]bool{
	"error": true, "path": true, "kind": true, "code": true, "source": true, "provider": true,
	"state": true, "platform": true, "status": true, "reason": true, "check": true, "name": true,
}

func (m message) identity() string {
	var b strings.Builder
	b.WriteString(string(m.severity))
	b.WriteString("|")
	b.WriteString(m.title)
	for _, f := range m.fields {
		if identityKeys[f.key] {
			b.WriteString("|")
			b.WriteString(f.key)
			b.WriteString("=")
			b.WriteString(f.value)
		}
	}
	return b.String()
}

// Run — жеткізу жұмысшысы: контекст жабылғанша кезекті босатады, аяқталғанда
// қалған хабарламаларды қысқа мерзімде жіберіп үлгеруге тырысады.
func (n *Notifier) Run(ctx context.Context) {
	if !n.Enabled() {
		return
	}
	summary := time.NewTicker(time.Minute)
	defer summary.Stop()
	for {
		select {
		case <-ctx.Done():
			n.drain()
			return
		case m := <-n.queue:
			n.deliver(ctx, m)
		case <-summary.C:
			n.flushSummary()
		}
	}
}

// drain — тоқтату кезінде: кезектегі хабарламаларға 3 секунд.
func (n *Notifier) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		select {
		case m := <-n.queue:
			n.deliver(ctx, m)
		default:
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// flushSummary — шектеу не толы кезек арқылы жоғалған ескертулер туралы бір хабарлама.
func (n *Notifier) flushSummary() {
	n.mu.Lock()
	count := n.suppressed
	n.suppressed = 0
	if count > 0 {
		n.tokens-- // the summary itself spends a token; it may go negative briefly
	}
	n.mu.Unlock()
	if count == 0 {
		return
	}
	select {
	case n.queue <- message{severity: Warning, title: "alerts suppressed by rate limit", at: n.now(),
		fields: []field{{"dropped", strconv.Itoa(count)}, {"window", "1m"}}}:
	default:
	}
}

// deliver — sendMessage; 429 мен 5xx-те қайталау, 400-де HTML-сіз нұсқа.
func (n *Notifier) deliver(ctx context.Context, m message) {
	richText, plainText := n.render(m)
	text, parseMode := richText, "HTML"
	for attempt := 0; attempt < 3; attempt++ {
		status, retryAfter, err := n.send(ctx, text, parseMode)
		var wait time.Duration
		switch {
		case err == nil && status == http.StatusOK:
			return
		case err == nil && status == http.StatusBadRequest && parseMode != "":
			// Entity parsing failed on an odd value: send it as plain text.
			text, parseMode = plainText, ""
			continue
		case err == nil && status == http.StatusTooManyRequests:
			wait = retryAfter
			if wait <= 0 {
				wait = 5 * time.Second
			}
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
		case err != nil || status >= 500:
			wait = time.Duration(attempt+1) * 2 * time.Second
		default:
			// 401/403/404: wrong token or chat; retrying cannot help.
			n.log.Warn("telegram alert rejected", "status", status, "severity", string(m.severity), "title", m.title)
			return
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
	n.log.Warn("telegram alert not delivered", "severity", string(m.severity), "title", m.title)
}

// send — бір HTTP сұраныс. Қатені журналға жазғанда токен жасырылады.
func (n *Notifier) send(ctx context.Context, text, parseMode string) (status int, retryAfter time.Duration, err error) {
	payload := map[string]any{
		"chat_id": n.cfg.ChatID, "text": text, "disable_web_page_preview": true,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		n.cfg.APIBase+"/bot"+n.cfg.BotToken+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		n.log.Warn("telegram request failed", "error", n.redact(err.Error()))
		return 0, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusTooManyRequests {
		var parsed struct {
			Parameters struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}
		if json.Unmarshal(raw, &parsed) == nil && parsed.Parameters.RetryAfter > 0 {
			retryAfter = time.Duration(parsed.Parameters.RetryAfter) * time.Second
		}
	}
	return resp.StatusCode, retryAfter, nil
}

func (n *Notifier) redact(s string) string {
	if n.cfg.BotToken == "" {
		return s
	}
	return strings.ReplaceAll(s, n.cfg.BotToken, "***")
}

// render — HTML және қарапайым мәтін нұсқалары.
func (n *Notifier) render(m message) (richText, plainText string) {
	var rich, plain strings.Builder
	head := fmt.Sprintf("%s %s · %s", m.severity.icon(), m.severity, n.cfg.Service)
	if n.cfg.Environment != "" {
		head += " · " + n.cfg.Environment
	}
	fmt.Fprintf(&rich, "%s <b>%s</b> · %s", m.severity.icon(), m.severity, html.EscapeString(n.cfg.Service))
	if n.cfg.Environment != "" {
		fmt.Fprintf(&rich, " · %s", html.EscapeString(n.cfg.Environment))
	}
	plain.WriteString(head)
	fmt.Fprintf(&rich, "\n<b>%s</b>", html.EscapeString(m.title))
	fmt.Fprintf(&plain, "\n%s", m.title)
	for _, f := range m.fields {
		fmt.Fprintf(&rich, "\n%s: <code>%s</code>", html.EscapeString(f.key), html.EscapeString(f.value))
		fmt.Fprintf(&plain, "\n%s: %s", f.key, f.value)
	}
	if m.repeats > 0 {
		line := fmt.Sprintf("repeated %d more time(s) in the last %s", m.repeats, n.cfg.DedupWindow)
		fmt.Fprintf(&rich, "\n<i>%s</i>", line)
		fmt.Fprintf(&plain, "\n%s", line)
	}
	stamp := m.at.Format("2006-01-02 15:04:05 MST")
	if n.host != "" {
		stamp += " · " + n.host
	}
	fmt.Fprintf(&rich, "\n<i>%s</i>", html.EscapeString(stamp))
	fmt.Fprintf(&plain, "\n%s", stamp)
	return truncate(rich.String(), maxTextRunes), truncate(plain.String(), maxTextRunes)
}

// ---------------------------------------------------------------- scrubbing

var (
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	tokenPattern  = regexp.MustCompile(`[A-Za-z0-9_\-:.]{32,}`)
	bearerPattern = regexp.MustCompile(`(?i)(bearer|basic)\s+[A-Za-z0-9._\-+/=]+`)
	digitsPattern = regexp.MustCompile(`\d{10,}`)
	spacePattern  = regexp.MustCompile(`\s+`)
	// skipKeys — alert messages never carry these even though logs may (stack traces are long).
	skipKeys = map[string]bool{"stack": true, "time": true, "level": true, "msg": true, logging.AlertKey: true,
		"user_id": true, "installation_id": true, "device_id": true}
)

// Scrub — сырттан келген қысқа мәтінді журналға жазу алдында тазалау (пошта, токен, ұзын сан).
func Scrub(v string) string { return scrubValue(v) }

// scrubValue — ескертуге түспес бұрын мәнді тазалау: пошта, токен, ұзын сан, жаңа жол.
func scrubValue(v string) string {
	v = bearerPattern.ReplaceAllString(v, "$1 [redacted]")
	v = emailPattern.ReplaceAllString(v, "[email]")
	v = tokenPattern.ReplaceAllString(v, "[redacted]")
	v = digitsPattern.ReplaceAllString(v, "[number]")
	v = spacePattern.ReplaceAllString(strings.TrimSpace(v), " ")
	return truncate(v, maxValueRunes)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func allowedKey(key string) bool {
	k := strings.ToLower(key)
	return k != "" && !logging.Forbidden(k) && !skipKeys[k]
}

func fieldsFromKV(kv []any) []field {
	var out []field
	for i := 0; i+1 < len(kv) && len(out) < maxFields; i += 2 {
		key, ok := kv[i].(string)
		if !ok || !allowedKey(key) {
			continue
		}
		out = append(out, field{key: key, value: scrubValue(stringify(kv[i+1]))})
	}
	return out
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return t
	case error:
		return t.Error()
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(v)
	}
}

// ---------------------------------------------------------------- slog hook

// Handler — журнал өңдеушісін орайды: Error деңгейі ERROR, LevelCritical — CRITICAL
// ескертуіне айналады. Өшірілген Notifier-де next өзгеріссіз қайтады.
func (n *Notifier) Handler(next slog.Handler) slog.Handler {
	if !n.Enabled() {
		return next
	}
	return &handler{next: next, n: n}
}

type handler struct {
	next   slog.Handler
	n      *Notifier
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	err := h.next.Handle(ctx, r)
	severity := Error
	switch {
	case r.Level >= logging.LevelCritical:
		severity = Critical
	case r.Level >= slog.LevelError:
	case r.Level >= slog.LevelWarn && flagged(r, h.attrs):
		severity = Warning
	default:
		return err
	}
	fields := make([]field, 0, len(h.attrs)+r.NumAttrs()+1)
	prefix := strings.Join(h.groups, ".")
	add := func(a slog.Attr) {
		key := a.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		if !allowedKey(a.Key) || len(fields) >= maxFields {
			return
		}
		fields = append(fields, field{key: key, value: scrubValue(stringify(a.Value.Resolve().Any()))})
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(a)
		return true
	})
	if id := logging.RequestID(ctx); id != "" && len(fields) < maxFields {
		fields = append(fields, field{key: "request_id", value: scrubValue(id)})
	}
	h.n.enqueue(message{severity: severity, title: r.Message, fields: fields})
	return err
}

// flagged — Warn жазбасында logging.Alert() бар ма.
func flagged(r slog.Record, inherited []slog.Attr) bool {
	for _, a := range inherited {
		if a.Key == logging.AlertKey {
			return true
		}
	}
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == logging.AlertKey {
			found = true
		}
		return !found
	})
	return found
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &handler{next: h.next.WithAttrs(attrs), n: h.n, attrs: merged, groups: h.groups}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := make([]string, 0, len(h.groups)+1)
	groups = append(groups, h.groups...)
	groups = append(groups, name)
	return &handler{next: h.next.WithGroup(name), n: h.n, attrs: h.attrs, groups: groups}
}

// SendNow — бір хабарламаны синхронды жібереді (-alert-test командасы): кезек те, шектеу де жоқ.
func (n *Notifier) SendNow(ctx context.Context, severity Severity, title string, kv ...any) error {
	if !n.Enabled() {
		return errors.New("telegram alerts are disabled (TELEGRAM_ALERTS_ENABLED=false)")
	}
	richText, _ := n.render(message{severity: severity, title: title, fields: fieldsFromKV(kv), at: n.now()})
	status, _, err := n.send(ctx, richText, "HTML")
	if err != nil {
		return fmt.Errorf("telegram request failed: %s", n.redact(err.Error()))
	}
	if status != http.StatusOK {
		return fmt.Errorf("telegram answered HTTP %d: check TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID and that the bot is a member of the chat", status)
	}
	return nil
}

// ---------------------------------------------------------------- emergency

// Emergency — іске қосылу құлағанда: баптау оқылмаса да TELEGRAM_* ортадан алынып,
// бір CRITICAL хабарлама синхронды (5 секунд) жіберіледі. Токен ешқашан шықпайды.
func Emergency(title string, err error, env string) {
	enabled, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("TELEGRAM_ALERTS_ENABLED")))
	token, chat := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")), strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	if !enabled || token == "" || chat == "" {
		return
	}
	n := New(Config{Enabled: true, BotToken: token, ChatID: chat, Environment: env,
		Client: &http.Client{Timeout: 5 * time.Second}}, nil)
	m := message{severity: Critical, title: title, at: n.now()}
	if err != nil {
		m.fields = []field{{"error", scrubValue(err.Error())}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n.deliver(ctx, m)
}
