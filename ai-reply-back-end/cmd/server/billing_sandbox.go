package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aireply/ai-reply-back-end/config"
)

// The test API shares the HTTPS listener, not the database or account sessions.
func billingSandboxConfig(primary config.Config) (config.Config, error) {
	base, err := url.Parse(primary.App.PublicBaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		(base.Path != "" && base.Path != "/") || (base.Scheme != "https" && base.Scheme != "http") ||
		(primary.App.IsProduction() && base.Scheme != "https") {
		return config.Config{}, fmt.Errorf("billing sandbox requires a valid PUBLIC_BASE_URL without a path")
	}
	if primary.Database.Path == "" || primary.Database.Path == ":memory:" {
		return config.Config{}, fmt.Errorf("billing sandbox requires a separate persistent database")
	}
	sandbox := primary
	ext := filepath.Ext(primary.Database.Path)
	sandbox.Database.Path = strings.TrimSuffix(primary.Database.Path, ext) + "-sandbox" + ext
	mainFile, mainErr := os.Stat(primary.Database.Path)
	testFile, testErr := os.Stat(sandbox.Database.Path)
	if mainErr == nil && testErr == nil && os.SameFile(mainFile, testFile) {
		return config.Config{}, fmt.Errorf("billing sandbox database must not alias the primary database")
	}
	base.Path = "/sandbox"
	sandbox.App.PublicBaseURL = strings.TrimRight(base.String(), "/")
	sandbox.App.Env = "staging"
	sandbox.App.SimulatorEnabled = false
	sandbox.Auth.AccessSecret = sandboxSecret(primary.Auth.AccessSecret, "access")
	sandbox.Auth.RefreshSecret = sandboxSecret(primary.Auth.RefreshSecret, "refresh")
	sandbox.Auth.Issuer += "-sandbox"
	sandbox.Auth.DemoMode, sandbox.Auth.LegacyEnabled = false, false
	sandbox.Auth.LegacySecret = ""
	sandbox.Admin.CookieName += "_sandbox"
	sandbox.Payments.Mode, sandbox.Payments.DemoCheckout = "off", false
	sandbox.Push.Enabled, sandbox.Push.WorkerEnabled, sandbox.Push.EmailsEnabled = false, false, false
	sandbox.Push.FCM = config.FCM{}
	// Apple sign-in uses the real authorization for this bundle, even in
	// TestFlight. Keep public client IDs for verification, but never revoke it
	// when deleting an account from the isolated billing database.
	sandbox.OAuth.AppleTeamID, sandbox.OAuth.AppleKeyID, sandbox.OAuth.ApplePrivateKey = "", "", ""
	sandbox.IAP.AppleEnabled, sandbox.IAP.AppleAcceptSandbox = true, true
	sandbox.IAP.GoogleClientEmail, sandbox.IAP.GooglePrivateKey, sandbox.IAP.GoogleServiceAccountFile = "", "", ""
	sandbox.IAP.GoogleVoidedPoll = 0
	sandbox.BCCSandboxNotify.Enabled = false
	sandbox.BCCCheckout.Enabled = primary.BillingSandboxBCCEnabled
	sandbox.BCCCheckout.AppEnv = "staging"
	sandbox.BCCCheckout.PublicBaseURL = sandbox.App.PublicBaseURL
	sandbox.BillingSandboxEnabled, sandbox.BillingSandboxBCCEnabled = false, false
	if problems := sandbox.Validate(); len(problems) > 0 {
		return config.Config{}, fmt.Errorf("billing sandbox configuration: %s", strings.Join(problems, "; "))
	}
	return sandbox, nil
}

func sandboxSecret(primary, purpose string) string {
	mac := hmac.New(sha256.New, []byte(primary))
	_, _ = mac.Write([]byte("ai-reply-billing-sandbox-v1:" + purpose))
	return hex.EncodeToString(mac.Sum(nil))
}
