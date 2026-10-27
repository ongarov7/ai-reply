package bcc

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMoneyHasExactMinorUnitsAndOverflowBoundaries(t *testing.T) {
	for _, test := range []struct {
		minor int64
		want  string
	}{{0, "0.00"}, {1, "0.01"}, {99, "0.99"}, {100, "1.00"}, {35000, "350.00"}, {math.MaxInt64, "92233720368547758.07"}} {
		got, err := FormatKZT(test.minor)
		if err != nil || got != test.want {
			t.Fatalf("format %d = %q, %v; want %q", test.minor, got, err, test.want)
		}
		minor, err := ParseKZT(test.want)
		if err != nil || minor != test.minor {
			t.Fatalf("parse %q = %d, %v; want %d", test.want, minor, err, test.minor)
		}
	}
	if _, err := FormatKZT(-1); !errors.Is(err, ErrInvalidAmount) {
		t.Fatal("negative amounts accepted")
	}
	for _, bad := range []string{"", "350", "1.0", "1.000", "01.00", "+1.00", "-1.00", "1,00", " 1.00", "1.00\n", "1e2", "NaN", "92233720368547758.08"} {
		if _, err := ParseKZT(bad); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("non-canonical or overflowing money accepted: %q", bad)
		}
	}
}

func TestEnvironmentAndEndpointAreExplicitAndBound(t *testing.T) {
	for _, test := range []struct {
		env      Environment
		endpoint string
	}{{Sandbox, SandboxEndpoint}, {Production, ProductionEndpoint}} {
		if endpoint, err := test.env.Endpoint(); err != nil || endpoint != test.endpoint {
			t.Fatalf("unexpected endpoint %q, %v", endpoint, err)
		}
		if err := ValidateEndpoint(test.env, test.endpoint); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{ProductionEndpoint, "http://test3ds.bcc.kz:5445/cgi-bin/cgi_link", SandboxEndpoint + "?token=secret", SandboxEndpoint + "#x",
		"https://user:secret@test3ds.bcc.kz:5445/cgi-bin/cgi_link", "https://test3ds.bcc.kz.evil.example:5445/cgi-bin/cgi_link", "https://test3ds.bcc.kz/cgi-bin/cgi_link", "https://127.0.0.1/"} {
		if err := ValidateEndpoint(Sandbox, endpoint); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("non-allowlisted endpoint accepted: %q", endpoint)
		}
	}
	for _, env := range []Environment{"", "test", "prod", "Sandbox"} {
		if _, err := env.Endpoint(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("unsupported environment accepted: %q", env)
		}
	}
	for _, cfg := range []Config{
		{Environment: Production, Terminal: "88888881"},
		{Environment: Production, Terminal: "88888888"},
		{Environment: Production, Terminal: "12345678", MACKeyHex: strings.ToLower(publicTestKey)},
		{Environment: Sandbox, Terminal: "88888881", Endpoint: ProductionEndpoint},
		{Environment: Sandbox, Terminal: "88888881", Timeout: 31 * time.Second},
		{Environment: Sandbox, Terminal: "88888881", Timeout: -time.Second},
	} {
		if _, err := New(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("unsafe configuration accepted: %v", err)
		}
	}
	cfg := Config{Environment: Sandbox, Terminal: "88888881", MACKeyHex: publicTestKey}
	if strings.Contains(fmt.Sprintf("%+v %#v", cfg, cfg), publicTestKey) {
		t.Fatal("configuration formatting revealed MAC key")
	}
}
