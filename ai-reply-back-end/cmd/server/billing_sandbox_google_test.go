package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
)

func TestBillingSandboxGoogleOptInDoesNotEnablePrimaryOrReuseItsSecrets(t *testing.T) {
	primary := billingPrimaryFixture(t)
	primary.IAP.GooglePackageName = "kz.yerek.aireply"
	primary.IAP.GoogleTokenSecret = strings.Repeat("p", 40)
	before := primary.IAP
	primary.BillingSandboxGoogleEnabled = true
	primary.BillingSandboxGoogle = config.IAP{GooglePackageName: "kz.yerek.aireply", GoogleClientEmail: "sandbox@example.invalid",
		GooglePrivateKey: "explicit-sandbox-fixture-key", GoogleServiceAccountFile: "/run/secrets/sandbox-play.json",
		GoogleTokenSecret: strings.Repeat("s", 40), GoogleVoidedPoll: 15 * time.Minute}
	sandbox, err := billingSandboxConfig(primary)
	if err != nil {
		t.Fatal(err)
	}
	if primary.IAP.Google() || !reflect.DeepEqual(primary.IAP, before) || !sandbox.IAP.Google() ||
		!sandbox.IAP.GoogleAcceptTest || !sandbox.IAP.GoogleTestOnly || sandbox.IAP.GoogleTokenSecret != strings.Repeat("s", 40) ||
		sandbox.IAP.GoogleClientEmail != "sandbox@example.invalid" || sandbox.IAP.GoogleVoidedPoll != 15*time.Minute ||
		sandbox.BillingSandboxGoogleEnabled || sandbox.BillingSandboxGoogle.Google() ||
		sandbox.Database.Path == primary.Database.Path || sandbox.Auth.AccessSecret == primary.Auth.AccessSecret {
		t.Fatal("sandbox Google opt-in changed primary or failed to isolate its state")
	}
	primary.BillingSandboxGoogleEnabled = false
	disabled, err := billingSandboxConfig(primary)
	if err != nil || disabled.IAP.Google() || disabled.IAP.GoogleTokenSecret != "" || disabled.IAP.GoogleVoidedPoll != 0 {
		t.Fatal("disabled sandbox retained Google credentials")
	}
}
