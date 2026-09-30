package reqctx

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseKeepsOnlySafeValues(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.Header.Set(HeaderRequestID, "req_client_0001")
	r.Header.Set(HeaderPlatform, " Android ")
	r.Header.Set(HeaderAppVersion, "1.3.2")
	r.Header.Set(HeaderAppBuild, "142")
	r.Header.Set(HeaderOSVersion, "17.5 (21F79)")
	r.Header.Set(HeaderInstallationID, "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
	r.Header.Set(HeaderSessionID, "sess;drop")
	r.Header.Set(HeaderTraceParent, "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01")

	c := Parse(r)
	if c.RequestID != "req_client_0001" || c.Platform != "android" || c.AppVersion != "1.3.2" ||
		c.AppBuild != "142" || c.OSVersion != "17.5 (21F79)" ||
		c.InstallationID != "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d" {
		t.Fatalf("parsed = %+v", c)
	}
	if c.SessionID != "" {
		t.Fatal("an id with unsafe characters must be dropped")
	}
	if c.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", c.TraceID)
	}
}

func TestParseRejectsUnknownAndOversizedValues(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(HeaderPlatform, "windows")
	r.Header.Set(HeaderAppVersion, strings.Repeat("9", 33))
	r.Header.Set(HeaderOSVersion, "<script>")
	r.Header.Set(HeaderTraceParent, "00-00000000000000000000000000000000-00f067aa0ba902b7-01")
	c := Parse(r)
	if c.Platform != "" || c.AppVersion != "" || c.OSVersion != "" || c.TraceID != "" {
		t.Fatalf("parsed = %+v", c)
	}
	if !strings.HasPrefix(c.RequestID, "req_") {
		t.Fatalf("a missing request id is generated, got %q", c.RequestID)
	}
}

func TestRequestIDOrNew(t *testing.T) {
	for _, bad := range []string{"", "short", "has space in it", "line\nbreak-000", strings.Repeat("a", 65), `quote"000000`} {
		if got := RequestIDOrNew(bad); !strings.HasPrefix(got, "req_") || got == bad {
			t.Errorf("RequestIDOrNew(%q) = %q", bad, got)
		}
	}
	for _, good := range []string{"req_0123456789", "01J8Z6Q3W6Y8K2M4N6P8R0T2V4", "a1b2c3d4:e5f6.g7-h8"} {
		if got := RequestIDOrNew(good); got != good {
			t.Errorf("RequestIDOrNew(%q) = %q", good, got)
		}
	}
	if RequestIDOrNew("") == RequestIDOrNew("") {
		t.Fatal("generated ids must differ")
	}
}

func TestObservedIsNilSafe(t *testing.T) {
	var none *Observed
	none.SetUser("u1")
	if none.User() != "" || ObservedFrom(context.Background()) != nil {
		t.Fatal("a missing collector must be a no-op")
	}
	o := &Observed{}
	ctx := WithObserved(context.Background(), o)
	ObservedFrom(ctx).SetUser("u2")
	if o.User() != "u2" {
		t.Fatal("the handler's user must reach the access log")
	}
	if From(context.Background()) != (Client{}) {
		t.Fatal("no client metadata means an empty value")
	}
}
