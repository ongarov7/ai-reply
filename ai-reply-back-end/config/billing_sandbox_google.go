package config

import (
	"strings"
	"time"
)

// The same Google Developer API serves license-test and real purchases. Its
// credentials are selected explicitly for /sandbox, never inherited from the
// primary Google Play or Firebase configuration.
func billingSandboxGoogleFromEnv() IAP {
	i := IAP{GooglePackageName: str("BILLING_SANDBOX_GOOGLE_PACKAGE_NAME", "kz.yerek.aireply"),
		GoogleTokenSecret: str("BILLING_SANDBOX_GOOGLE_TOKEN_SECRET", ""),
		GoogleAcceptTest:  true, GoogleTestOnly: true, GoogleVoidedPoll: time.Hour}
	if !boolean("BILLING_SANDBOX_GOOGLE_ENABLED", false) {
		return i
	}
	if raw := str("BILLING_SANDBOX_GOOGLE_VOIDED_POLL", "1h"); raw != "" {
		poll, err := time.ParseDuration(raw)
		if err != nil || poll < 15*time.Minute || poll > 24*time.Hour {
			i.problems = append(i.problems, "BILLING_SANDBOX_GOOGLE_VOIDED_POLL must be between 15m and 24h")
		} else {
			i.GoogleVoidedPoll = poll
		}
	}
	path := str("BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE", "")
	if path == "" {
		i.problems = append(i.problems, "BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE is required when sandbox Google Play is enabled")
		return i
	}
	account, err := readGoogleServiceAccount(path, "BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE", false)
	if err != nil {
		i.problems = append(i.problems, err.Error())
		return i
	}
	i.GoogleClientEmail, i.GooglePrivateKey, i.GoogleServiceAccountFile = account.ClientEmail, account.PrivateKey, path
	return i
}

func (c Config) validateBillingSandboxGoogle() []string {
	if !c.BillingSandboxGoogleEnabled {
		return nil
	}
	var problems []string
	if !c.BillingSandboxEnabled {
		problems = append(problems, "BILLING_SANDBOX_GOOGLE_ENABLED requires BILLING_SANDBOX_ENABLED=true")
	}
	for _, problem := range c.BillingSandboxGoogle.validate() {
		problems = append(problems, "billing sandbox: "+strings.ReplaceAll(problem, "GOOGLE_PLAY_", "BILLING_SANDBOX_GOOGLE_"))
	}
	if !c.BillingSandboxGoogle.Google() || c.BillingSandboxGoogle.GoogleServiceAccountFile == "" {
		problems = append(problems, "BILLING_SANDBOX_GOOGLE_ENABLED requires its explicit service-account FILE")
	}
	if len(c.BillingSandboxGoogle.GoogleTokenSecret) < 32 {
		problems = append(problems, "BILLING_SANDBOX_GOOGLE_TOKEN_SECRET must be at least 32 characters")
	}
	for _, primary := range []string{c.Auth.AccessSecret, c.Auth.RefreshSecret, c.IAP.GoogleTokenSecret} {
		if primary != "" && c.BillingSandboxGoogle.GoogleTokenSecret == primary {
			problems = append(problems, "BILLING_SANDBOX_GOOGLE_TOKEN_SECRET must be separate from primary application and purchase-token secrets")
			break
		}
	}
	if c.BillingSandboxGoogle.GoogleVoidedPoll < 15*time.Minute || c.BillingSandboxGoogle.GoogleVoidedPoll > 24*time.Hour {
		problems = append(problems, "BILLING_SANDBOX_GOOGLE_VOIDED_POLL must be between 15m and 24h")
	}
	return problems
}
