package config

import (
	"strings"
	"testing"
)

func TestMacOSReleaseURL(t *testing.T) {
	for _, test := range []struct {
		url string
		ok  bool
	}{
		{"", true},
		{"https://example.test/downloads/macos/AI-Reply-1.0.0.dmg", true},
		{"https://example.test:443/downloads/macos/AI-Reply-1.0.0.dmg", true},
		{"https://other.test/downloads/macos/AI-Reply-1.0.0.dmg", false},
		{"https://example.test.evil.test/downloads/macos/AI-Reply-1.0.0.dmg", false},
		{"http://example.test/downloads/macos/AI-Reply-1.0.0.dmg", false},
		{"https://user:secret@example.test/downloads/macos/AI-Reply-1.0.0.dmg", false},
		{"https://example.test:8443/downloads/macos/AI-Reply-1.0.0.dmg", false},
		{"https://example.test/downloads/macos/../secrets/file.dmg", false},
		{"https://example.test/downloads/macos/%2e%2e/file.dmg", false},
		{"https://example.test/downloads/macos/file.dmg?secret=value", false},
		{"https://example.test/downloads/macos/file.dmg?", false},
		{"https://example.test/downloads/macos/file.dmg#fragment", false},
		{"https://example.test/downloads/macos/file.zip", false},
		{"javascript:alert(1)", false},
	} {
		t.Run(test.url, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("PUBLIC_BASE_URL", "https://example.test")
			t.Setenv("MACOS_DOWNLOAD_URL", test.url)
			cfg, err := Load("")
			if test.ok {
				if err != nil || cfg.App.MacOSInstallerURL() != test.url {
					t.Fatalf("valid release URL rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "MACOS_DOWNLOAD_URL") {
				t.Fatalf("invalid release URL accepted: %v", err)
			}
		})
	}
}
