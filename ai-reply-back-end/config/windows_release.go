package config

import (
	"net/url"
	"path"
	"strings"
)

// WindowsInstallerURL exposes only an explicitly published installer on our host.
// Empty keeps the preview download unavailable; it does not imply a signed release.
func (a App) WindowsInstallerURL() string {
	u, err := url.Parse(a.WindowsDownloadURL)
	base, baseErr := url.Parse(a.PublicBaseURL)
	if err != nil || baseErr != nil || u == nil || base == nil ||
		u.Scheme != "https" || u.Hostname() == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) ||
		u.Port() != "" && u.Port() != "443" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.RawPath != "" || path.Clean(u.Path) != u.Path ||
		!strings.HasPrefix(u.Path, "/downloads/windows/") || !strings.HasSuffix(u.Path, ".exe") ||
		strings.ContainsAny(a.WindowsDownloadURL, "%#\\ \t\r\n\"<>") {
		return ""
	}
	return a.WindowsDownloadURL
}
