// Package sandboxnotify records BCC sandbox notifications for bank acceptance
// tests. It cannot grant purchases, update application payments, or call BCC.
package sandboxnotify

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
)

const MaxBodyBytes = 64 << 10

var (
	ErrInvalid  = errors.New("sandbox notify: invalid fields")
	ErrUnknown  = errors.New("sandbox notify: unknown test order")
	ErrMismatch = errors.New("sandbox notify: fields do not match registered test")
)

type Store struct{ db *database.DB }

// Open uses a separate private SQLite file, with FULL synchronous durability.
// The caller must not point it at an application database.
func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := database.Open(database.Options{Path: path, MaxReadConns: 2, FullSync: true})
	if err != nil {
		return nil, err
	}
	if _, err = db.Writer().Exec(`PRAGMA synchronous=FULL;
CREATE TABLE IF NOT EXISTS bcc_sandbox_attempts (
 order_id TEXT NOT NULL, transaction_type TEXT NOT NULL, amount TEXT NOT NULL,
 currency TEXT NOT NULL, terminal TEXT NOT NULL, notify_url TEXT NOT NULL,
 request_json TEXT NOT NULL, registered_at TEXT NOT NULL,
 PRIMARY KEY(order_id,transaction_type));
CREATE TABLE IF NOT EXISTS bcc_sandbox_notifications (
 digest TEXT PRIMARY KEY, order_id TEXT NOT NULL, transaction_type TEXT NOT NULL,
 received_at TEXT NOT NULL, transport_auth TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state='received_unverified'),
 fields_json TEXT NOT NULL, raw_body TEXT NOT NULL,
 FOREIGN KEY(order_id,transaction_type) REFERENCES bcc_sandbox_attempts(order_id,transaction_type));
CREATE INDEX IF NOT EXISTS bcc_sandbox_notifications_order ON bcc_sandbox_notifications(order_id);
`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func validFields(fields map[string]string) error {
	if len(fields) == 0 || len(fields) > 128 {
		return ErrInvalid
	}
	for k, v := range fields {
		if len(k) == 0 || len(k) > 64 || len(v) > 8192 || !utf8.ValidString(k) || !utf8.ValidString(v) {
			return ErrInvalid
		}
		for _, c := range k {
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
				return ErrInvalid
			}
		}
		switch strings.ToUpper(k) {
		case "CARD", "PAN", "CARD_NUMBER", "CARDNUMBER", "CVC", "CVC2", "CVV", "CVV2", "EXP", "EXP_YEAR", "EXP_MONTH", "PASSWORD", "PASSWD", "PSW_RENEW", "CARD_EXPIRY":
			return ErrInvalid
		}
	}
	return nil
}

func orderOK(order string) bool {
	if len(order) < 6 || len(order) > 32 {
		return false
	}
	for _, c := range order {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func canonicalAmount(value string) (string, error) {
	if !strings.Contains(value, ".") {
		value += ".00"
	} else {
		parts := strings.Split(value, ".")
		if len(parts) != 2 {
			return "", ErrInvalid
		}
		if len(parts[1]) == 1 {
			value += "0"
		}
	}
	v, err := bcc.ParseKZT(value)
	if err != nil || v != 10000 {
		return "", ErrInvalid
	}
	return bcc.FormatKZT(v)
}

// Register stores the exact merchant fields before the browser submits them.
// Re-registering identical fields is safe; changing an existing order is refused.
func (s *Store) Register(ctx context.Context, fields map[string]string, publicURL string) error {
	if err := validFields(fields); err != nil {
		return err
	}
	if !orderOK(fields["ORDER"]) || fields["TERMINAL"] != "88888881" || fields["CURRENCY"] != "398" || fields["NOTIFY_URL"] != publicURL {
		return ErrInvalid
	}
	if fields["TRTYPE"] == "1" && fields["MERCHANT"] != "00000001" || fields["MERCHANT"] != "" && fields["MERCHANT"] != "00000001" {
		return ErrInvalid
	}
	if !hexField(fields["NONCE"], 16, 64) || !hexField(fields["P_SIGN"], 40, 40) {
		return ErrInvalid
	}
	switch fields["TRTYPE"] {
	case "1", "171", "14", "22":
	default:
		return ErrInvalid
	}
	amount, err := canonicalAmount(fields["AMOUNT"])
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT request_json FROM bcc_sandbox_attempts WHERE order_id=? AND transaction_type=?`, fields["ORDER"], fields["TRTYPE"]).Scan(&existing)
		if err == nil {
			if existing != string(encoded) {
				return ErrMismatch
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bcc_sandbox_attempts(order_id,transaction_type,amount,currency,terminal,notify_url,request_json,registered_at) VALUES(?,?,?,?,?,?,?,?)`, fields["ORDER"], fields["TRTYPE"], amount, "398", "88888881", publicURL, string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
}

type Record struct {
	Digest        string            `json:"digest"`
	Order         string            `json:"order"`
	ReceivedAt    string            `json:"receivedAt"`
	TransportAuth string            `json:"transportAuth"`
	State         string            `json:"state"`
	Fields        map[string]string `json:"fields"`
	RawBody       string            `json:"rawBody"`
}

// Record commits only a callback bound to a pre-registered public sandbox test.
// Transport authentication and a matching request do not verify payment success.
func (s *Store) Record(ctx context.Context, fields map[string]string, raw, auth string) (bool, error) {
	if err := validFields(fields); err != nil {
		return false, err
	}
	if len(raw) > MaxBodyBytes || !orderOK(fields["ORDER"]) {
		return false, ErrInvalid
	}
	if auth != "public_endpoint" && auth != "http_basic" {
		return false, ErrInvalid
	}
	if fields["ACTION"] == "" || fields["RC"] == "" {
		return false, ErrInvalid
	}
	if len(fields["ACTION"]) > 16 || len(fields["RC"]) > 16 {
		return false, ErrInvalid
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return false, err
	}
	parsed, err := ParseForm(raw)
	if err != nil {
		return false, err
	}
	rawFields, err := json.Marshal(parsed)
	if err != nil || string(rawFields) != string(encoded) {
		return false, ErrInvalid
	}
	// Canonical scalar fields make reordered form repeats idempotent.
	hash := sha256.Sum256(encoded)
	digest := hex.EncodeToString(hash[:])
	duplicate := false
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var kind, amount, currency, terminal, notifyURL, registeredJSON string
		err := tx.QueryRowContext(ctx, `SELECT transaction_type,amount,currency,terminal,notify_url,request_json FROM bcc_sandbox_attempts WHERE order_id=? AND transaction_type=?`, fields["ORDER"], fields["TRTYPE"]).Scan(&kind, &amount, &currency, &terminal, &notifyURL, &registeredJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknown
		}
		if err != nil {
			return err
		}
		var expected map[string]string
		if err := json.Unmarshal([]byte(registeredJSON), &expected); err != nil {
			return err
		}
		// NONCE and the original request P_SIGN are matching evidence only.
		// The public sandbox key cannot authenticate the notifying bank.
		for _, key := range []string{"NONCE", "P_SIGN", "MERCH_RN_ID", "MERCH_TRAN_STATE"} {
			want, exists := expected[key]
			if !exists && (key == "MERCH_RN_ID" || key == "MERCH_TRAN_STATE") {
				continue
			}
			got := fields[key]
			wantHash, gotHash := sha256.Sum256([]byte(want)), sha256.Sum256([]byte(got))
			if want == "" || subtle.ConstantTimeCompare(wantHash[:], gotHash[:]) != 1 {
				return ErrMismatch
			}
		}
		value := fields["AMOUNT"]
		if value == "" {
			value = fields["TRAN_AMOUNT"]
		}
		got, err := canonicalAmount(value)
		if err != nil || got != amount || fields["CURRENCY"] != currency || fields["TERMINAL"] != terminal || fields["TRTYPE"] != kind || kind == "1" && fields["MERCHANT"] != "00000001" || fields["MERCHANT"] != "" && fields["MERCHANT"] != "00000001" {
			return ErrMismatch
		}
		if fields["NOTIFY_URL"] != "" && fields["NOTIFY_URL"] != notifyURL {
			return ErrMismatch
		}
		if other := fields["TRAN_AMOUNT"]; other != "" {
			n, err := canonicalAmount(other)
			if err != nil || n != amount {
				return ErrMismatch
			}
		}
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT digest FROM bcc_sandbox_notifications WHERE digest=?`, digest).Scan(&previous)
		if err == nil {
			duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bcc_sandbox_notifications WHERE order_id=?`, fields["ORDER"]).Scan(&count); err != nil {
			return err
		}
		if count >= 16 {
			return ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bcc_sandbox_notifications(digest,order_id,transaction_type,received_at,transport_auth,state,fields_json,raw_body) VALUES(?,?,?,?,?,?,?,?)`, digest, fields["ORDER"], fields["TRTYPE"], time.Now().UTC().Format(time.RFC3339Nano), auth, "received_unverified", string(encoded), raw)
		return err
	})
	return duplicate, err
}

func (s *Store) Records(ctx context.Context, order string) ([]Record, error) {
	if order != "" && !orderOK(order) {
		return nil, ErrInvalid
	}
	query := `SELECT digest,order_id,received_at,transport_auth,state,fields_json,raw_body FROM bcc_sandbox_notifications`
	var args []any
	if order != "" {
		query += ` WHERE order_id=?`
		args = append(args, order)
	}
	query += ` ORDER BY received_at,digest`
	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var fields string
		if err := rows.Scan(&r.Digest, &r.Order, &r.ReceivedAt, &r.TransportAuth, &r.State, &fields, &r.RawBody); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(fields), &r.Fields); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ParseForm rejects ambiguous query/body combinations and duplicate scalar keys.
func ParseForm(raw string) (map[string]string, error) {
	if len(raw) > MaxBodyBytes {
		return nil, ErrInvalid
	}
	parsed, err := url.ParseQuery(raw)
	if err != nil {
		return nil, ErrInvalid
	}
	fields := make(map[string]string, len(parsed))
	for k, v := range parsed {
		if len(v) != 1 {
			return nil, ErrInvalid
		}
		fields[k] = v[0]
	}
	if err := validFields(fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// String deliberately excludes notification fields and configuration secrets.
func (s *Store) String() string { return "bcc sandbox notification store" }

func hexField(value string, min, max int) bool {
	if len(value) < min || len(value) > max || len(value)%2 != 0 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
