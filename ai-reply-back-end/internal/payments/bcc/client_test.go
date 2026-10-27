package bcc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func testClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	c, err := New(Config{Environment: Sandbox, Terminal: "88888881", MACKeyHex: publicTestKey})
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = transport
	c.now = func() time.Time { return time.Date(2026, 10, 27, 18, 42, 48, 0, time.UTC) }
	return c
}

func formResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestQueryUsesStoredAttemptAndFreshSignedUTCRequest(t *testing.T) {
	var calls []url.Values
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != SandboxEndpoint || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("query did not target the fixed form endpoint")
		}
		body, _ := io.ReadAll(r.Body)
		fields, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fields)
		return formResponse(r, "ACTION=0&RC=00&ORDER=123456&TRTYPE=90"), nil
	})
	for range 2 {
		response, err := c.Query(context.Background(), QueryRequest{Order: "123456", OriginalType: Purchase, StartedAt: c.now().Add(-time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if status, err := response.OriginalPaymentStatus(); status != StatusUnknown || !errors.Is(err, ErrOriginalStatusUnknown) {
			t.Fatal("a successful query was treated as a paid purchase")
		}
	}
	for _, fields := range calls {
		if fields.Get("TRTYPE") != "90" || fields.Get("TRAN_TRTYPE") != "1" || fields.Get("ORDER") != "123456" || fields.Get("TERMINAL") != "88888881" || fields.Get("TIMESTAMP") != "20261027184248" || len(fields.Get("NONCE")) != 32 {
			t.Fatal("query was not bound to the stored attempt and UTC timestamp")
		}
		want, err := SignRequest(StatusQuery, fields, publicTestKey)
		if err != nil || fields.Get("P_SIGN") != want {
			t.Fatal("outgoing query MAC was not valid")
		}
	}
	if len(calls) != 2 || calls[0].Get("NONCE") == calls[1].Get("NONCE") {
		t.Fatal("each query needs a fresh nonce and exactly one HTTP call")
	}
}

func TestQueryRejectsInvalidAttemptAndExpiredWindowWithoutNetwork(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid query performed network I/O")
		return nil, nil
	})
	for _, test := range []struct {
		request QueryRequest
		want    error
	}{
		{QueryRequest{Order: "123456", OriginalType: Purchase}, ErrInvalidRequest},
		{QueryRequest{Order: "uuid-not-bank-order", OriginalType: Purchase, StartedAt: c.now()}, ErrInvalidRequest},
		{QueryRequest{Order: "123456", OriginalType: Purchase, StartedAt: c.now().Add(time.Second)}, ErrInvalidRequest},
		{QueryRequest{Order: "123456", OriginalType: Purchase, StartedAt: c.now().Add(-24 * time.Hour)}, ErrQueryWindowExpired},
		{QueryRequest{Order: "123456", OriginalType: StatusQuery, StartedAt: c.now()}, ErrUnsupportedTransaction},
		{QueryRequest{Order: "123456", OriginalType: "999", StartedAt: c.now()}, ErrUnsupportedTransaction},
	} {
		if _, err := c.Query(context.Background(), test.request); !errors.Is(err, test.want) {
			t.Fatalf("invalid query error = %v, want %v", err, test.want)
		}
	}
}

func TestHealthIsUnsignedAndTransportIsBounded(t *testing.T) {
	c, err := New(Config{Environment: Sandbox, Terminal: "88888888"})
	if err != nil {
		t.Fatal(err)
	}
	transport := c.http.Transport.(*http.Transport)
	if c.http.Timeout <= 0 || c.http.Timeout > maxTimeout || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unbounded or insecure default transport")
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		fields, _ := url.ParseQuery(string(body))
		if fields.Get("TRTYPE") != "800" || fields.Get("TERMINAL") != "88888888" || fields.Has("P_SIGN") || fields.Has("BACKREF") || fields.Has("NOTIFY_URL") {
			t.Fatal("health must be the documented unsigned connection check")
		}
		// Sanitized representation of the observed shared-sandbox health format.
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"ACTION":"0","RC":"00","TERMINAL":"88888888"}`)), Request: r}, nil
	})
	if response, err := c.Health(context.Background()); err != nil || response.HTTPStatus != http.StatusOK || response.Action != "0" || response.RC != "00" {
		t.Fatalf("connection check response not preserved: %v", err)
	}
}

func TestClientDoesNotFollowRedirectsRetryOrExposeBodies(t *testing.T) {
	calls := 0
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://untrusted.example/"}},
			Body: io.NopCloser(strings.NewReader("secret-token-body")), Request: r}, nil
	})
	if response, err := c.Health(context.Background()); !errors.Is(err, ErrUnavailable) || response.HTTPStatus != http.StatusFound || calls != 1 {
		t.Fatalf("redirect followed or retried: %d calls, %v", calls, err)
	}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-token-transport-error") })
	if _, err := c.Health(context.Background()); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("raw transport error leaked")
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return formResponse(r, strings.Repeat("x", maxResponseBytes+1)), nil
	})
	if _, err := c.Health(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized response accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	if _, err := c.Health(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation lost: %v", err)
	}
}

func TestClientFormattingDoesNotRevealMACBytes(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) { return nil, ErrUnavailable })
	for _, value := range []any{c, *c} {
		formatted := fmt.Sprintf("%v %+v %#v", value, value, value)
		for _, secret := range []string{publicTestKey, strings.ToLower(publicTestKey), fmt.Sprintf("%v", c.key), fmt.Sprintf("%#v", c.key)} {
			if strings.Contains(formatted, secret) {
				t.Fatal("client formatting revealed MAC key material")
			}
		}
	}
}
