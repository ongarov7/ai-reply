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
	"time"

	"github.com/aireply/ai-reply-back-end/config"
)

// The test API shares the HTTPS listener, not the database or account sessions.
func billingSandboxConfig(primary config.Config) (config.Config, error) {
	macOSInstaller := primary.BillingSandboxMacOSInstallerURL()
	if primary.BillingSandboxMacOSDownloadURL != "" && macOSInstaller == "" {
		return config.Config{}, fmt.Errorf("BILLING_SANDBOX_MACOS_DOWNLOAD_URL must be a valid separately published installer, different from MACOS_DOWNLOAD_URL")
	}
	if primary.BillingSandboxPushEnabled && !primary.Push.FCM.Configured() {
		return config.Config{}, fmt.Errorf("billing sandbox push requires configured Firebase credentials")
	}
	if primary.BillingSandboxWebTopupOfferEnabled && !primary.BillingSandboxPushEnabled {
		return config.Config{}, fmt.Errorf("billing sandbox web offer requires explicitly enabled sandbox push")
	}
	if google := primary.BillingSandboxGoogle; primary.BillingSandboxGoogleEnabled &&
		(!google.Google() || google.GoogleServiceAccountFile == "" || len(google.GoogleTokenSecret) < 32 ||
			google.GoogleVoidedPoll < 15*time.Minute || google.GoogleVoidedPoll > 24*time.Hour) {
		return config.Config{}, fmt.Errorf("billing sandbox Google Play requires its explicit service-account FILE, stable token secret and voided polling")
	}
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
	sandbox.App.MacOSDownloadURL = macOSInstaller
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
	sandbox.Push.WebTopupOfferEnabled = false
	sandbox.Push.FCM = config.FCM{}
	if primary.BillingSandboxPushEnabled {
		sandbox.Push.Enabled, sandbox.Push.WorkerEnabled = true, true
		sandbox.Push.FCM = primary.Push.FCM
		sandbox.Push.WebTopupOfferEnabled = primary.BillingSandboxWebTopupOfferEnabled
	}
	// Apple sign-in uses the real authorization for this bundle, even in
	// TestFlight. Keep public client IDs for verification, but never revoke it
	// when deleting an account from the isolated billing database.
	sandbox.OAuth.AppleTeamID, sandbox.OAuth.AppleKeyID, sandbox.OAuth.ApplePrivateKey = "", "", ""
	sandbox.IAP.AppleEnabled, sandbox.IAP.AppleAcceptSandbox = true, true
	sandbox.IAP.GoogleClientEmail, sandbox.IAP.GooglePrivateKey, sandbox.IAP.GoogleServiceAccountFile = "", "", ""
	sandbox.IAP.GoogleVoidedPoll = 0
	sandbox.IAP.GoogleTokenSecret = ""
	sandbox.IAP.GoogleAcceptTest, sandbox.IAP.GoogleTestOnly = false, false
	if primary.BillingSandboxGoogleEnabled {
		google := primary.BillingSandboxGoogle
		sandbox.IAP.GooglePackageName = google.GooglePackageName
		sandbox.IAP.GoogleClientEmail, sandbox.IAP.GooglePrivateKey = google.GoogleClientEmail, google.GooglePrivateKey
		sandbox.IAP.GoogleServiceAccountFile, sandbox.IAP.GoogleTokenSecret = google.GoogleServiceAccountFile, google.GoogleTokenSecret
		sandbox.IAP.GoogleVoidedPoll = google.GoogleVoidedPoll
		sandbox.IAP.GoogleAcceptTest, sandbox.IAP.GoogleTestOnly = true, true
	}
	sandbox.BCCSandboxNotify.Enabled = false
	// Never inherit primary production merchant, signing or notification secrets.
	sandbox.BCCCheckout = primary.BCCSandboxCheckout
	sandbox.BCCCheckout.Enabled = primary.BillingSandboxBCCEnabled
	sandbox.BCCCheckout.Environment = "sandbox"
	sandbox.BCCCheckout.ProductionApproved = false
	sandbox.BCCCheckout.AppEnv = "staging"
	sandbox.BCCCheckout.PublicBaseURL = sandbox.App.PublicBaseURL
	sandbox.BillingSandboxEnabled, sandbox.BillingSandboxBCCEnabled = false, false
	sandbox.BillingSandboxGoogleEnabled, sandbox.BillingSandboxGoogle = false, config.IAP{}
	sandbox.BillingSandboxPushEnabled, sandbox.BillingSandboxWebTopupOfferEnabled = false, false
	sandbox.BillingSandboxMacOSDownloadURL = ""
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
