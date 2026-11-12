package config

import (
	"net/url"
	"path"
	"strings"
)

// MacOSInstallerURL exposes only an explicitly published installer on our host.
// Empty keeps the website in its pre-release state.
func (a App) MacOSInstallerURL() string {
	u, err := url.Parse(a.MacOSDownloadURL)
	base, baseErr := url.Parse(a.PublicBaseURL)
	if err != nil || baseErr != nil || u == nil || base == nil ||
		u.Scheme != "https" || u.Hostname() == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) ||
		u.Port() != "" && u.Port() != "443" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.RawPath != "" || path.Clean(u.Path) != u.Path ||
		!strings.HasPrefix(u.Path, "/downloads/macos/") || !strings.HasSuffix(u.Path, ".dmg") ||
		strings.ContainsAny(a.MacOSDownloadURL, " \t\r\n\"<>") {
		return ""
	}
	return a.MacOSDownloadURL
}

// BillingSandboxMacOSInstallerURL never falls back to the production download.
// The two clients use separate account databases and must be published separately.
func (c Config) BillingSandboxMacOSInstallerURL() string {
	app := c.App
	app.MacOSDownloadURL = c.BillingSandboxMacOSDownloadURL
	installer := app.MacOSInstallerURL()
	if installer == "" {
		return ""
	}
	testURL, _ := url.Parse(installer)
	mainURL, _ := url.Parse(c.App.MacOSInstallerURL())
	// Both validated URLs use the same host and HTTPS default port. Comparing
	// their path also catches case/explicit :443 variants of the same installer.
	if mainURL != nil && testURL.Path == mainURL.Path {
		return ""
	}
	return installer
}
