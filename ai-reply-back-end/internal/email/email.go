// Package email — транзакциялық хаттар: мазмұны және жеткізу провайдері.
//
// Handlers never talk to a provider directly. The auth service depends on the
// Sender interface only, so tests use a fake and a future provider change is
// one new implementation of it.
package email

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// OTPMessage — бір реттік кодты жеткізуге қажеттің бәрі.
type OTPMessage struct {
	// To — нормаланған алушы мекенжайы.
	To string
	// Code — 4 таңбалы код. Журналға ешқашан жазылмайды.
	Code string
	// Locale — хат тілі (kk | ru | en | uz).
	Locale string
	// TTL — код қанша уақыт жарамды.
	TTL time.Duration
	// Reference — провайдерге жіберілетін қайталанбайтын идентификатор
	// (OTP жазбасының ID-і): қайталап жіберу бір хатқа айналады.
	Reference string
	// Purpose — кодтың мақсаты (domain.OTPPurpose*): "delete" хаты тіркелгіні
	// жою коды екенін айтады, қалғаны — кіру коды.
	Purpose string
}

// Sender — хат жіберу абстракциясы.
type Sender interface {
	SendOTP(ctx context.Context, msg OTPMessage) error
}

// Translator — локаль мен кілт бойынша мәтін (localization.Bundle.T).
type Translator func(locale, key string) string

// ErrDelivery — провайдер хатты қабылдамады не қолжетімсіз.
var ErrDelivery = errors.New("email: delivery failed")

// Content — дайын хат.
type Content struct {
	Subject string
	Text    string
	HTML    string
}

// Аударма кілттері (internal/localization/locales/*.json).
const (
	keySubject    = "email.otp.subject"
	keyIntro      = "email.otp.intro"
	keyExpires    = "email.otp.expires"
	keyNeverShare = "email.otp.never_share"
	keyIgnore     = "email.otp.ignore"

	// Тіркелгіні жою коды: тақырып, кіріспе және «сұрамасаңыз» жолы өзгеше.
	keyDeleteSubject = "email.otp_delete.subject"
	keyDeleteIntro   = "email.otp_delete.intro"
	keyDeleteIgnore  = "email.otp_delete.ignore"
)

var otpHTML = template.Must(template.New("otp").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:0;background:#f5f5f7;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f5f5f7;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:16px;padding:32px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1d1d1f;">
<tr><td style="font-size:18px;font-weight:600;padding-bottom:24px;">{{.Brand}}</td></tr>
<tr><td style="font-size:15px;line-height:22px;padding-bottom:12px;">{{.Intro}}</td></tr>
<tr><td style="font-size:36px;font-weight:700;letter-spacing:10px;padding:4px 0 20px;font-family:'SF Mono',Menlo,Consolas,monospace;">{{.Code}}</td></tr>
<tr><td style="font-size:14px;line-height:20px;color:#6e6e73;padding-bottom:8px;">{{.Expires}}</td></tr>
<tr><td style="font-size:14px;line-height:20px;color:#6e6e73;padding-bottom:8px;">{{.NeverShare}}</td></tr>
<tr><td style="font-size:14px;line-height:20px;color:#6e6e73;">{{.Ignore}}</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

// RenderOTP — коды бар хаттың тақырыбы, мәтіндік және HTML нұсқасы.
//
// The plain-text part is always present: some clients and most spam filters
// look at it, and it is what screen readers fall back to.
func RenderOTP(t Translator, brand, locale, code string, ttl time.Duration) (Content, error) {
	return RenderOTPFor(t, brand, locale, code, ttl, "")
}

// RenderOTPFor — RenderOTP, мақсатымен: "delete" — тіркелгіні жою коды.
func RenderOTPFor(t Translator, brand, locale, code string, ttl time.Duration, purpose string) (Content, error) {
	if t == nil {
		return Content{}, errors.New("email: translator is required")
	}
	if strings.TrimSpace(brand) == "" {
		brand = "AI Reply"
	}
	minutes := int(ttl.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	lang := domain.NormalizeLocale(strings.ToLower(strings.TrimSpace(locale)))
	view := struct {
		Lang, Brand, Subject, Intro, Code, Expires, NeverShare, Ignore string
	}{
		Lang:       lang,
		Brand:      brand,
		Subject:    t(lang, keySubject),
		Intro:      t(lang, keyIntro),
		Code:       code,
		Expires:    strings.ReplaceAll(t(lang, keyExpires), "{minutes}", strconv.Itoa(minutes)),
		NeverShare: t(lang, keyNeverShare),
		Ignore:     t(lang, keyIgnore),
	}
	if purpose == domain.OTPPurposeDelete {
		view.Subject, view.Intro, view.Ignore = t(lang, keyDeleteSubject), t(lang, keyDeleteIntro), t(lang, keyDeleteIgnore)
	}

	var html bytes.Buffer
	if err := otpHTML.Execute(&html, view); err != nil {
		return Content{}, err
	}
	text := strings.Join([]string{
		view.Brand, "", view.Intro, "", view.Code, "", view.Expires, view.NeverShare, "", view.Ignore, "",
	}, "\n")
	return Content{Subject: view.Subject, Text: text, HTML: html.String()}, nil
}
