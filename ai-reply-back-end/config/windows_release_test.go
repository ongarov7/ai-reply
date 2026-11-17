package config

import (
	"strings"
	"testing"
)

func TestWindowsReleaseURL(t *testing.T) {
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"", true},
		{"https://example.test/downloads/windows/AIReply-Setup-1.0.1.exe", true},
		{"https://EXAMPLE.TEST:443/downloads/windows/AIReply-Setup.exe", true},
		{"https://other.test/downloads/windows/AIReply-Setup.exe", false},
		{"https://example.test.evil.test/downloads/windows/AIReply-Setup.exe", false},
		{"http://example.test/downloads/windows/AIReply-Setup.exe", false},
		{"https://user:secret@example.test/downloads/windows/AIReply-Setup.exe", false},
		{"https://example.test:8443/downloads/windows/AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows/../private/AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows/./AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows//AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows/..\\private\\AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows/%2e%2e/AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows/%41IReply-Setup.exe", false},
		{"https://example.test/downloads/windows/AIReply%20Setup.exe", false},
		{"https://example.test/downloads/windows/AIReply-Setup.exe?token=value", false},
		{"https://example.test/downloads/windows/AIReply-Setup.exe?", false},
		{"https://example.test/downloads/windows/AIReply-Setup.exe#fragment", false},
		{"https://example.test/downloads/windows/AIReply-Setup.exe#", false},
		{"https://example.test/downloads/windows/AIReply-Setup.zip", false},
		{"https://example.test/downloads/macos/AIReply-Setup.exe", false},
		{"https://example.test/downloads/windows/", false},
		{"javascript:alert(1)", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("PUBLIC_BASE_URL", "https://example.test")
			t.Setenv("WINDOWS_DOWNLOAD_URL", tc.url)
			cfg, err := Load("")
			if tc.ok {
				if err != nil || cfg.App.WindowsInstallerURL() != tc.url {
					t.Fatalf("valid Windows installer rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "WINDOWS_DOWNLOAD_URL") {
				t.Fatalf("unsafe Windows installer accepted: %v", err)
			}
		})
	}
}
