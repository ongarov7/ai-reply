package apptest

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	reviewAddress = "review@ai-reply.kz"
	reviewCode    = "4826"
)

// withReviewLogin — App Review / Play review тексерушісінің кіруі қосулы харнесс.
func withReviewLogin(opts ...harnessOption) []harnessOption {
	return append([]harnessOption{withEnv("REVIEW_LOGIN_EMAIL", reviewAddress), withEnv("REVIEW_LOGIN_CODE", reviewCode)}, opts...)
}

// Тексерушінің поштасына хатсыз белгілі код; аккаунт — қарапайым тегін аккаунт; журналда пошта жоқ.
func TestReviewLoginCodeWorksForTheReviewAddressOnly(t *testing.T) {
	h := newHarness(t, withReviewLogin()...)
	mailer := withMailer(h, 1357)

	res := h.requestEmailCode("Review@AI-Reply.kz")
	mustStatus(t, res, http.StatusOK, "")
	if mailer.count() != 0 {
		t.Fatal("no mail goes to the review address")
	}
	if _, demo := res.body["demo_mode"]; demo || res.num("code_length") != 4 || res.num("resend_after") != 32 {
		t.Fatalf("the review challenge must look like any other: %s", res.raw)
	}
	if got := h.text(`SELECT channel FROM otp_codes WHERE identity_value = ?`, reviewAddress); got != "review" {
		t.Fatalf("channel = %q", got)
	}
	verify := h.verifyEmailCode(reviewAddress, reviewCode)
	mustStatus(t, verify, http.StatusOK, "")
	if isNew, _ := verify.body["is_new_user"].(bool); !isNew || verify.str("subscription", "plan", "code") != "free" ||
		verify.str("user", "email") != reviewAddress {
		t.Fatalf("the reviewer gets a normal free account: %s", verify.raw)
	}
	logs := h.logs.String()
	// The code is looked for as a JSON value: four digits alone also turn up inside random ids.
	if !strings.Contains(logs, "review login code issued") || strings.Contains(logs, reviewAddress) ||
		strings.Contains(logs, `"`+reviewCode+`"`) {
		t.Fatal("the review code is logged without the address or the code")
	}

	// Any other address gets a random code by mail; the review code does not open it.
	other := "someone@example.com"
	mustStatus(t, h.requestEmailCode(other), http.StatusOK, "")
	if mailer.count() != 1 || mailer.last(t).Code != "1357" {
		t.Fatalf("other addresses get a mailed random code: %d", mailer.count())
	}
	mustStatus(t, h.verifyEmailCode(other, reviewCode), http.StatusBadRequest, "INVALID_OTP")
	mustStatus(t, h.verifyEmailCode(other, "1357"), http.StatusOK, "")
}

// Тексерушінің кодына да күту уақыты, шектер және әрекет саны қолданылады.
func TestReviewLoginKeepsEveryLimit(t *testing.T) {
	h := newHarness(t, withReviewLogin(withEnv("RATE_OTP_REQUEST_PER_DAY", "2"))...)
	withMailer(h)

	mustStatus(t, h.requestEmailCode(reviewAddress), http.StatusOK, "")
	mustStatus(t, h.requestEmailCode(reviewAddress), http.StatusTooManyRequests, "OTP_RESEND_COOLDOWN")
	for i := 0; i < 4; i++ {
		mustStatus(t, h.verifyEmailCode(reviewAddress, "0000"), http.StatusBadRequest, "INVALID_OTP")
	}
	mustStatus(t, h.verifyEmailCode(reviewAddress, "0000"), http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED")
	mustStatus(t, h.verifyEmailCode(reviewAddress, reviewCode), http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED")

	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(reviewAddress), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode(reviewAddress, reviewCode), http.StatusOK, "")
	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(reviewAddress), http.StatusTooManyRequests, "RATE_LIMITED")
}

// Бір поштаға тәулігіне қате енгізу шегі: барлық кодтар қосылады, IP қанша болса да.
func TestFailedCodeGuessesAreCappedPerAddress(t *testing.T) {
	h := newHarness(t, withEnv("OTP_MAX_FAILED_PER_DAY", "6"))
	withMailer(h, 1111, 2222, 3333, 4444)
	address := "guesser@example.com"

	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	for i := 0; i < 4; i++ {
		mustStatus(t, h.verifyEmailCode(address, "9999"), http.StatusBadRequest, "INVALID_OTP")
	}
	mustStatus(t, h.verifyEmailCode(address, "9999"), http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED")

	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "") // 5 failures so far
	mustStatus(t, h.verifyEmailCode(address, "9999"), http.StatusBadRequest, "INVALID_OTP")

	// Six wrong guesses in a day: even the right code waits until the first code leaves the window.
	locked := h.verifyEmailCode(address, "2222")
	mustStatus(t, locked, http.StatusTooManyRequests, "RATE_LIMITED")
	if want := float64(24*3600 - 33); locked.num("error", "details", "retry_after_seconds") != want ||
		locked.header.Get("Retry-After") != "86367" {
		t.Fatalf("retry after: %s / %q", locked.raw, locked.header.Get("Retry-After"))
	}
	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusTooManyRequests, "RATE_LIMITED")
	if n := h.scalar(`SELECT COUNT(*) FROM otp_codes WHERE identity_value = ?`, address); n != 2 {
		t.Fatalf("a locked address got a new code: %d codes", n)
	}

	// Another address is not affected.
	mustStatus(t, h.requestEmailCode("other-guesser@example.com"), http.StatusOK, "")

	// Once the first code's failures leave the 24 hours, the address works again.
	h.clock.Advance(24*time.Hour - 66*time.Second + time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	mustStatus(t, h.verifyEmailCode(address, "4444"), http.StatusOK, "")
}

// Жеткізілмеген код («delivery_failed») поштаның сағаттық та, тәуліктік те шегіне кірмейді.
func TestUndeliveredCodesDoNotCountTowardTheCaps(t *testing.T) {
	h := newHarness(t, withEnv("RATE_OTP_REQUEST_PER_DAY", "2"), withEnv("RATE_OTP_REQUEST_PER_ADDRESS_PER_HOUR", "2"))
	mailer := withMailer(h, 1001, 1002, 1003, 1004, 1005)
	address := "outage-caps@example.com"

	mailer.setFailure(errors.New("resend: responded 503"))
	for i := 0; i < 3; i++ {
		mustStatus(t, h.requestEmailCode(address), http.StatusServiceUnavailable, "EMAIL_DELIVERY_FAILED")
	}
	mailer.setFailure(nil)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusOK, "")
	h.clock.Advance(33 * time.Second)
	mustStatus(t, h.requestEmailCode(address), http.StatusTooManyRequests, "RATE_LIMITED")
}
