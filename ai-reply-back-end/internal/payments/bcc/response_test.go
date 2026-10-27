package bcc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGatewayWrapperSuccessNeverProvesOriginalPayment(t *testing.T) {
	for _, body := range []struct{ contentType, content string }{
		{"application/json", `{"ACTION":"0","RC":"00","ORDER":"123456","TERMINAL":"88888881","RRN":"123456789012"}`},
		{"application/x-www-form-urlencoded", "ACTION=0&RC=00&ORDER=123456&TERMINAL=88888881&TRTYPE=90&RRN=123456789012&P_SIGN=untrusted&MERCH_TOKEN_ID=secret"},
		{"text/html; charset=UTF-8", `<html><body><form><input type="hidden" name="ACTION" value="0"><input name="RC" value="00"><input name="ORDER" value="123456"><input name="RRN" value="123456789012"></form></body></html>`},
		{"text/html", `<form><input name=ACTION value=0><input name=RC value=00><input name=ORDER value=123456></form>`},
	} {
		response, err := ParseResponse(StatusQuery, body.contentType, []byte(body.content))
		if err != nil || response.Action != "0" || response.RC != "00" || response.Order != "123456" {
			t.Fatalf("gateway fields not preserved: %v", err)
		}
		status, err := response.OriginalPaymentStatus()
		if status != StatusUnknown || !errors.Is(err, ErrOriginalStatusUnknown) {
			t.Fatalf("status-query wrapper incorrectly became a payment result: %q, %v", status, err)
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", response, response), "123456789012") {
			t.Fatal("accidental formatting revealed financial reference")
		}
	}
}

// A live sandbox query on 2026-10-27 returned these field names (without TRTYPE,
// but with TRAN_TRTYPE). Values here are synthetic and encoded for the supported
// form decoder, not a claim about the bank's HTTP wire format or schema guarantee.
func TestObservedSandboxOriginalFieldsRemainUnverified(t *testing.T) {
	response, err := ParseResponse(StatusQuery, "application/x-www-form-urlencoded", []byte(
		"ACTION=0&RC=00&TRAN_TRTYPE=1&TERMINAL=88888881&ORDER=123456789&AMOUNT=350.00&CURRENCY=398&RRN=123456789012&INT_REF=ABCDEF0123456789&P_SIGN=echoed-original-request-signature&MERCH_TOKEN_ID=not-retained"))
	if err != nil || response.ReportedOriginalType != "1" || response.TRType != "" || response.Amount != "350.00" || response.Currency != CurrencyKZT {
		t.Fatalf("observed original-operation metadata not preserved: %v", err)
	}
	if status, err := response.OriginalPaymentStatus(); status != StatusUnknown || !errors.Is(err, ErrOriginalStatusUnknown) {
		t.Fatal("one observed fixture must not become an undocumented settlement contract")
	}
}

func TestResponseParserRejectsAmbiguousAndUnsupportedBodies(t *testing.T) {
	for _, test := range []struct{ contentType, body string }{
		{"application/json", `{"ACTION":"0","RC":"00","ACTION":"2"}`},
		{"application/json", `{"ACTION":"0","RC":"00","UNKNOWN":"a","UNKNOWN":"b"}`},
		{"application/json", `{"ACTION":"0","RC":"00"} {"ACTION":"2"}`},
		{"application/json", `{"ACTION":"0","RC":"00"} trailing`},
		{"application/json", `{"ACTION":0,"RC":"00"}`},
		{"application/json", `{"ACTION":"0","RC":null}`},
		{"application/json", `{"ACTION":"0","RC":"00","UNKNOWN":{}}`},
		{"application/json", `[{"ACTION":"0","RC":"00"}]`},
		{"text/plain", "ACTION=0&RC=00"},
		{"application/x-www-form-urlencoded; charset=windows-1251", "ACTION=0&RC=00"},
		{"application/x-www-form-urlencoded", "ACTION=0&ACTION=2&RC=00"},
		{"application/x-www-form-urlencoded", "ACTION=0&RC=00&UNKNOWN=a&UNKNOWN=b"},
		{"application/x-www-form-urlencoded", "ACTION=0&RC=%zz"},
		{"application/x-www-form-urlencoded", "ACTION=0"},
		{"application/x-www-form-urlencoded", "ACTION=0&RC=%0A00"},
		{"text/html", `<input name="ACTION" value="0"><input name="ACTION" value="2"><input name="RC" value="00">`},
		{"text/html", `<input name="ACTION" value="0" value="2"><input name="RC" value="00">`},
		{"text/html", `<input name="ACTION" value="0"><input name="RC" value="00"><script>if (a < b) {}</script>`},
	} {
		if response, err := ParseResponse(StatusQuery, test.contentType, []byte(test.body)); !errors.Is(err, ErrUnsupportedResponse) {
			t.Fatalf("unsupported or ambiguous response accepted: %q, %v", test.body, err)
		} else if status, _ := response.OriginalPaymentStatus(); status != StatusUnknown {
			t.Fatal("invalid response acquired a payment status")
		}
	}
	if _, err := ParseResponse(StatusQuery, "text/html", make([]byte, maxResponseBytes+1)); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal("oversized response accepted")
	}
	if _, err := ParseResponse(StatusQuery, "application/x-www-form-urlencoded", []byte("ACTION=0&RC=00&ORDER="+strings.Repeat("1", maxFieldValue+1))); !errors.Is(err, ErrUnsupportedResponse) {
		t.Fatal("oversized field accepted")
	}
}
