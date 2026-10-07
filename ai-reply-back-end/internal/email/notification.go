package email

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Outgoing — дайын хат (хабарлама хаттары үшін).
type Outgoing struct {
	// To — алушы мекенжайы (жіберу сәтінде тіркелгіден оқылады).
	To      string
	Content Content
	// IdempotencyKey — провайдерге: бір кілт — бір хат, қайталау екіншісін жібермейді.
	IdempotencyKey string
	// Reference — X-Entity-Ref-ID: әр хат пошта клиентінде жеке тізбек болады.
	Reference string
}

// ErrTemplateMissing — бұл хабарлама түрінің хат мәтіні аудармаларда жоқ.
var ErrTemplateMissing = errors.New("email: no template for this notification type")

// NotificationTemplates — хабарлама хаттары аударма кілттерінен құрылады.
//
// For a notification type T the keys are, in the order they appear:
//
//	email.T.subject   required; also the heading
//	email.T.greeting  optional
//	email.T.body      required; paragraphs separated by a blank line
//	email.T.expires   optional; e.g. "valid until {date}"
//	email.T.manage    optional; where to see or change it in the app
//	email.T.footer    optional; why this e-mail was sent, in small print
//
// "{name}" placeholders take the notification's params. An optional line
// whose placeholder has no value is left out, so "expires" disappears for a
// plan without an end date. A type without subject and body has no e-mail
// (ErrTemplateMissing): adding a new kind of mail is adding keys to every
// locale file.
type NotificationTemplates struct {
	Translate Translator
	Brand     string
}

var placeholder = regexp.MustCompile(`\{[a-z][a-z0-9_]*\}`)

// The layout is the sign-in code e-mail's: the same card, brand line and colours.
var notificationHTML = template.Must(template.New("notification").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:0;background:#f5f5f7;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f5f5f7;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#ffffff;border-radius:16px;padding:32px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1d1d1f;">
<tr><td style="font-size:18px;font-weight:600;padding-bottom:24px;">{{.Brand}}</td></tr>
<tr><td style="font-size:20px;font-weight:600;line-height:26px;padding-bottom:16px;">{{.Subject}}</td></tr>
{{with .Greeting}}<tr><td style="font-size:15px;line-height:22px;padding-bottom:12px;">{{.}}</td></tr>
{{end}}{{range .Paragraphs}}<tr><td style="font-size:15px;line-height:22px;padding-bottom:12px;">{{.}}</td></tr>
{{end}}{{with .Expires}}<tr><td style="font-size:15px;line-height:22px;font-weight:600;padding-bottom:12px;">{{.}}</td></tr>
{{end}}{{with .Manage}}<tr><td style="font-size:14px;line-height:20px;color:#6e6e73;padding-top:8px;">{{.}}</td></tr>
{{end}}</table>
{{with .Footer}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;">
<tr><td style="font-size:12px;line-height:18px;color:#86868b;padding:16px 32px 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">{{.}}</td></tr>
</table>
{{end}}</td></tr>
</table>
</body>
</html>
`))

// Render — хабарлама түрінің хаты, алушының тілінде.
func (n NotificationTemplates) Render(kind, locale string, params map[string]string) (Content, error) {
	if n.Translate == nil {
		return Content{}, errors.New("email: translator is required")
	}
	brand := strings.TrimSpace(n.Brand)
	if brand == "" {
		brand = "AI Reply"
	}
	lang := domain.NormalizeLocale(strings.ToLower(strings.TrimSpace(locale)))
	pairs := make([]string, 0, 2*len(params))
	for name, value := range params {
		pairs = append(pairs, "{"+name+"}", value)
	}
	fill := strings.NewReplacer(pairs...) // one pass: a value is never read as a placeholder
	// text — the key's translation with params filled; ok=false when the key
	// is missing (the bundle answers a missing key with the key itself),
	// missing names a placeholder that has no value.
	text := func(part string) (value string, ok bool, missing string) {
		key := "email." + kind + "." + part
		v := strings.TrimSpace(n.Translate(lang, key))
		if v == "" || v == key {
			return "", false, ""
		}
		for _, p := range placeholder.FindAllString(v, -1) {
			if params[strings.Trim(p, "{}")] == "" {
				return "", true, p
			}
		}
		return fill.Replace(v), true, ""
	}
	required := func(part string) (string, error) {
		v, ok, missing := text(part)
		switch {
		case !ok:
			return "", ErrTemplateMissing
		case missing != "":
			return "", fmt.Errorf("email: %s.%s has no value for %s", kind, part, missing)
		}
		return v, nil
	}
	optional := func(part string) string {
		if v, ok, missing := text(part); ok && missing == "" {
			return v
		}
		return ""
	}

	subject, err := required("subject")
	if err != nil {
		return Content{}, err
	}
	body, err := required("body")
	if err != nil {
		return Content{}, err
	}
	var paragraphs []string
	for _, p := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paragraphs = append(paragraphs, p)
		}
	}
	view := struct {
		Lang, Brand, Subject, Greeting string
		Paragraphs                     []string
		Expires, Manage, Footer        string
	}{
		Lang: lang, Brand: brand, Subject: subject, Greeting: optional("greeting"), Paragraphs: paragraphs,
		Expires: optional("expires"), Manage: optional("manage"), Footer: optional("footer"),
	}

	var html bytes.Buffer
	if err := notificationHTML.Execute(&html, view); err != nil {
		return Content{}, err
	}
	blocks := []string{brand, subject}
	if view.Greeting != "" {
		blocks = append(blocks, view.Greeting)
	}
	blocks = append(blocks, paragraphs...)
	for _, line := range []string{view.Expires, view.Manage} {
		if line != "" {
			blocks = append(blocks, line)
		}
	}
	if view.Footer != "" {
		blocks = append(blocks, "--\n"+view.Footer)
	}
	return Content{Subject: subject, Text: strings.Join(blocks, "\n\n") + "\n", HTML: html.String()}, nil
}
