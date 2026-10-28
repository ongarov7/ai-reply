package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// BCCSandboxNotify captures public BCC sandbox callbacks on the existing API.
// It cannot enable production acquiring or purchase fulfillment.
type BCCSandboxNotify struct {
	Enabled         bool
	PublicURL       string
	ManagementToken string
	BasicUsername   string
	BasicPassword   string
}

func (b BCCSandboxNotify) String() string {
	return fmt.Sprintf("BCCSandboxNotify{Enabled:%t, ManagementConfigured:%t, BasicConfigured:%t}", b.Enabled, b.ManagementToken != "", b.BasicUsername != "" && b.BasicPassword != "")
}

func (b BCCSandboxNotify) GoString() string { return b.String() }

func (b BCCSandboxNotify) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Enabled              bool `json:"enabled"`
		ManagementConfigured bool `json:"managementConfigured"`
		BasicConfigured      bool `json:"basicConfigured"`
	}{b.Enabled, b.ManagementToken != "", b.BasicUsername != "" && b.BasicPassword != ""})
}

// Validate reports variable names only; supplied credentials are never echoed.
func (b BCCSandboxNotify) Validate(publicBaseURL string) []string {
	if !b.Enabled {
		return nil
	}
	var problems []string
	u, err := url.Parse(b.PublicURL)
	base, baseErr := url.Parse(publicBaseURL)
	if err != nil || baseErr != nil || u == nil || base == nil ||
		u.Scheme != "https" || u.Hostname() == "" || u.Port() != "443" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" ||
		u.Path != "/api/v1/payments/bcc/sandbox/notify" ||
		base.Scheme != "https" || !strings.EqualFold(u.Hostname(), base.Hostname()) ||
		net.ParseIP(u.Hostname()) != nil || !strings.Contains(u.Hostname(), ".") ||
		strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".local") {
		problems = append(problems, "BCC_TEST_NOTIFY_URL must be the PUBLIC_BASE_URL host over HTTPS with explicit :443 and path /api/v1/payments/bcc/sandbox/notify")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(b.ManagementToken)
	if err != nil || len(decoded) != 32 || len(b.ManagementToken) != 43 {
		problems = append(problems, "BCC_SANDBOX_NOTIFY_MANAGEMENT_TOKEN must encode 32 random bytes as unpadded base64url")
	}
	if (b.BasicUsername == "") != (b.BasicPassword == "") ||
		strings.ContainsAny(b.BasicUsername, ":\r\n\x00") || strings.ContainsAny(b.BasicPassword, "\r\n\x00") ||
		b.BasicUsername != "" && (len(b.BasicUsername) > 128 || len(b.BasicPassword) < 32 || len(b.BasicPassword) > 256) {
		problems = append(problems, "BCC_SANDBOX_NOTIFY_BASIC_USERNAME and BCC_SANDBOX_NOTIFY_BASIC_PASSWORD must be configured together with a password of at least 32 characters")
	}
	if b.BasicPassword != "" && b.BasicPassword == b.ManagementToken {
		problems = append(problems, "BCC sandbox notification and management credentials must differ")
	}
	return problems
}
