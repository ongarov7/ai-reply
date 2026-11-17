package api

import (
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
	"github.com/aireply/ai-reply-back-end/internal/users"
)

// The website cabinet's own endpoints (docs/web-cabinet-ai-context.md §5.4).
// They use the cookie session of /account and the same-origin checks of the
// purchase endpoints; nothing here is reachable with an app token.

func (a *webAccount) getAIContext(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	a.s.writeAIContext(w, r.Context(), user, nil)
}

func (a *webAccount) saveAIContext(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	a.s.saveAIContext(w, r, user, true)
}

// Usage-history window: the chart's days, today included.
const (
	usageHistoryDefaultDays = 30
	usageHistoryMinDays     = 7
	usageHistoryMaxDays     = 90
)

type usageDayDTO struct {
	Date    string `json:"date"`
	Replies int    `json:"replies"`
	Units   int    `json:"units"`
}

type usagePlatformDTO struct {
	Platform string `json:"platform"`
	Replies  int    `json:"replies"`
}

type usageHistoryResponse struct {
	// UserID — whose history this is (the page checks it against the account it shows).
	UserID       string             `json:"user_id"`
	Days         []usageDayDTO      `json:"days"`
	Platforms    []usagePlatformDTO `json:"platforms"`
	TotalReplies int                `json:"total_replies"`
	TotalUnits   int                `json:"total_units"`
	Timezone     string             `json:"timezone"`
}

// usageHistory — replies per day for the cabinet's chart: delivered replies
// and composed messages (ai_usage_events) and the replies they charged
// (usage_daily, the context surcharge included), plus where they were made.
func (a *webAccount) usageHistory(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	days := usageHistoryDefaultDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < usageHistoryMinDays || n > usageHistoryMaxDays {
			httpx.Fail(w, domain.InvalidField("days", "7 to 90"))
			return
		}
		days = n
	}
	loc := a.s.subs.Location()
	today := a.s.subs.Now().In(loc)
	first := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	out := usageHistoryResponse{UserID: user.ID, Days: make([]usageDayDTO, 0, days), Platforms: []usagePlatformDTO{}, Timezone: a.s.cfg.App.Timezone}
	index := make(map[string]int, days)
	for i := range days {
		date := first.AddDate(0, 0, i).Format("2006-01-02")
		index[date] = i
		out.Days = append(out.Days, usageDayDTO{Date: date})
	}
	units, err := a.s.subs.UsageUnitsByDay(r.Context(), user.ID, out.Days[0].Date, out.Days[len(out.Days)-1].Date)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	for date, n := range units {
		if i, ok := index[date]; ok && n > 0 {
			out.Days[i].Units = n
			out.TotalUnits += n
		}
	}
	stamps, err := a.s.subs.SuccessfulGenerations(r.Context(), user.ID, first)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	platforms := map[string]int{}
	for _, stamp := range stamps {
		i, ok := index[stamp.At.In(loc).Format("2006-01-02")]
		if !ok {
			continue
		}
		out.Days[i].Replies++
		out.TotalReplies++
		platforms[usagePlatform(stamp.Platform)]++
	}
	for platform, n := range platforms {
		out.Platforms = append(out.Platforms, usagePlatformDTO{Platform: platform, Replies: n})
	}
	sort.Slice(out.Platforms, func(i, j int) bool {
		if out.Platforms[i].Replies != out.Platforms[j].Replies {
			return out.Platforms[i].Replies > out.Platforms[j].Replies
		}
		return out.Platforms[i].Platform < out.Platforms[j].Platform
	})
	httpx.JSON(w, http.StatusOK, out)
}

// usagePlatform — the platform name the cabinet shows (what the apps send),
// anything unexpected as "other".
func usagePlatform(p string) string {
	switch p = strings.ToLower(strings.TrimSpace(p)); p {
	case domain.PlatformIOS, domain.PlatformAndroid, domain.PlatformMacOS, domain.PlatformWindows, domain.PlatformWeb, "watchos":
		return p
	default:
		return "other"
	}
}

// saveConsent — the cabinet accepts the current terms and privacy policy for a
// website-only account before its first microphone use (AI processing).
func (a *webAccount) saveConsent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TermsVersion   string `json:"terms_version"`
		PrivacyVersion string `json:"privacy_version"`
		Locale         string `json:"locale"`
		ExpectedUserID string `json:"expected_user_id"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	user, _ := UserFrom(r.Context())
	if body.ExpectedUserID != user.ID {
		accountChanged(w)
		return
	}
	consent, err := a.s.users.SaveLegalConsent(r.Context(), user.ID, users.LegalConsentInput{
		TermsVersion: body.TermsVersion, PrivacyVersion: body.PrivacyVersion, Locale: body.Locale,
		Platform: domain.PlatformWeb,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"legal_consent": toLegalConsentDTO(consent)})
}

// sameOriginUpload — the same-origin rule of the cabinet's JSON posts for its
// one multipart request: the site's Origin, the cabinet's X-Requested-With
// and a multipart body. A form on another site can send none of these.
func (a *webAccount) sameOriginUpload(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if strings.ToLower(r.Header.Get("Origin")) != a.origin || r.Header.Get("X-Requested-With") != "AIReplyAccount" ||
			err != nil || media != "multipart/form-data" {
			httpx.Error(w, http.StatusForbidden, "WEB_ORIGIN_REQUIRED", "Use the account page to continue.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// transcribe — the cabinet microphone: a recording about the user → text for
// their AI profile. Same audio rules, per-minute bucket and daily cap as the
// apps' voice input; it needs the current consent (the audio goes to the AI
// provider) and spends no replies.
func (a *webAccount) transcribe(w http.ResponseWriter, r *http.Request) {
	if !a.s.voiceEnabled() {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	user, _ := UserFrom(r.Context())
	form, err := readVoiceForm(w, r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	// A tab showing another account hears that first, whatever else is wrong.
	if form.expectedUserID != user.ID {
		accountChanged(w)
		return
	}
	if err := a.s.users.RequireConsent(r.Context(), user.ID); err != nil {
		httpx.Fail(w, err)
		return
	}
	result, err := a.s.ai.Transcribe(r.Context(), ai.TranscribeRequest{
		User:             user,
		RequestID:        form.requestID,
		Audio:            form.audio,
		ClientDurationMS: form.durationMS,
		Languages:        form.languages,
		Purpose:          ai.PurposeProfile,
		Website:          true,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	languages := result.Languages
	if languages == nil {
		languages = []string{}
	}
	httpx.JSON(w, http.StatusOK, transcribeResponse{
		Text:       result.Text,
		Languages:  languages,
		DurationMS: result.Duration.Milliseconds(),
	})
}
