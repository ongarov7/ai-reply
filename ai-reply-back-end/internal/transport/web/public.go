package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

func clientIP(r *http.Request, trustProxy bool) string { return httpx.ClientIP(r, trustProxy) }

type planView struct {
	Name        string
	Description string
	PriceText   string
	DailyLimit  int
	IsFree      bool
	Featured    bool
}

type landingView struct {
	baseView
	Plans []planView
	Boot  template.JS
	// FreeLimit — тегін тарифтің нақты күндік лимиті (0 — белгісіз, сан көрсетілмейді).
	FreeLimit int
	HeroNote  string
	// PricingSubtitle мен PricingCTA — бірде-бір тариф сатылмаса, тариф ауыстыру
	// не қосымшада таңдау туралы уәде бермейді (*_free нұсқалары).
	PricingSubtitle string
	PricingCTA      string
	// AppStoreURL, PlayStoreURL — дүкендегі бет (APP_STORE_URL, PLAY_STORE_URL); бос — «жақында».
	AppStoreURL  string
	PlayStoreURL string
}

// demoScene — лендингтегі анимацияланған мысал.
type demoScene struct {
	Incoming         string `json:"incoming"`
	Instruction      string `json:"instruction"`
	Reply            string `json:"reply"`
	LabelIncoming    string `json:"labelIncoming"`
	LabelInstruction string `json:"labelInstruction"`
	LabelReply       string `json:"labelReply"`
}

// demoScenes — үш сценарий: сатылым, кездесу, тапсырыс.
func (s *Server) demoScenes(locale string) []demoScene {
	prefixes := []string{"hero.demo", "hero.demo2", "hero.demo3"}
	scenes := make([]demoScene, 0, len(prefixes))
	for _, prefix := range prefixes {
		scenes = append(scenes, demoScene{
			Incoming:         s.bundle.T(locale, prefix+"_incoming"),
			Instruction:      s.bundle.T(locale, prefix+"_instruction"),
			Reply:            s.bundle.T(locale, prefix+"_reply"),
			LabelIncoming:    s.bundle.T(locale, "hero.demo_label_incoming"),
			LabelInstruction: s.bundle.T(locale, "hero.demo_label_instruction"),
			LabelReply:       s.bundle.T(locale, "hero.demo_label_reply"),
		})
	}
	return scenes
}

// handleLanding — басты бет.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	list, err := s.plans.Listed(r.Context())
	if err != nil {
		s.log.Error("landing plans failed", "error", err.Error())
	}

	boot, err := json.Marshal(map[string]any{"scenes": s.demoScenes(locale), "locale": locale})
	if err != nil {
		boot = []byte("{}")
	}
	view := landingView{baseView: s.base(locale), Boot: template.JS(boot),
		AppStoreURL: s.cfg.App.AppStoreURL, PlayStoreURL: s.cfg.App.PlayStoreURL}
	purchasable := false
	for _, p := range list {
		if p.IsFree && view.FreeLimit == 0 {
			view.FreeLimit = p.DailyLimit
		}
		purchasable = purchasable || (s.payments != nil && s.payments.Purchasable(r.Context(), p))
	}
	// The limit every new account starts with, even when the free plan is not listed.
	if plan, err := s.plans.Default(r.Context()); err == nil && plan.DailyLimit > 0 {
		view.FreeLimit = plan.DailyLimit
	}
	if view.FreeLimit > 0 {
		view.HeroNote = strings.ReplaceAll(s.bundle.T(locale, "hero.note"), "{limit}", strconv.Itoa(view.FreeLimit))
	}
	view.PricingSubtitle, view.PricingCTA = s.bundle.T(locale, "pricing.subtitle_free"), s.bundle.T(locale, "pricing.cta_free")
	if purchasable {
		view.PricingSubtitle, view.PricingCTA = s.bundle.T(locale, "pricing.subtitle"), s.bundle.T(locale, "pricing.cta")
	}
	for i, p := range list {
		view.Plans = append(view.Plans, planView{
			Name:        p.LocalizedName(locale),
			Description: p.LocalizedDescription(locale),
			PriceText:   traits.FormatMoney(p.Price, p.Currency),
			DailyLimit:  p.DailyLimit,
			IsFree:      p.IsFree,
			Featured:    i == 1,
		})
	}
	s.render(w, "landing", "public_layout", view)
}

type legalView struct {
	baseView
	Title   string
	Updated string
	Body    template.HTML
	Boot    template.JS
}

// handleTerms — пайдалану шарттары.
func (s *Server) handleTerms(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	s.render(w, "legal", "public_layout", legalView{
		baseView: s.base(locale),
		Title:    s.bundle.T(locale, "legal.terms.title"),
		Updated:  legal.UpdatedDate,
		Body:     template.HTML(termsBody(locale, s.legalInfo())),
		Boot:     template.JS("{}"),
	})
}

// handlePrivacy — құпиялық саясаты.
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	s.render(w, "legal", "public_layout", legalView{
		baseView: s.base(locale),
		Title:    s.bundle.T(locale, "legal.privacy.title"),
		Updated:  legal.UpdatedDate,
		Body:     template.HTML(privacyBody(locale, s.legalInfo())),
		Boot:     template.JS("{}"),
	})
}

// handleSupport — қолдау беті (дүкендердегі Support URL): байланыс, құжаттар, тіркелгіні жою.
func (s *Server) handleSupport(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	s.render(w, "support", "public_layout", legalView{
		baseView: s.base(locale),
		Title:    s.bundle.T(locale, "support.title"),
		Boot:     template.JS("{}"),
	})
}

type accountDeleteView struct {
	baseView
	CodeSent string
	// KeptBody — «не сақталады»: аккаунтсыз орнату қашан жойылатыны RETENTION_ANON_INSTALLATIONS_DAYS-тан.
	KeptBody string
	Boot     template.JS
}

// handleAccountDelete — тіркелгіні жою беті (Google Play «Delete account URL»): қолданбадағы
// жол, не жойылатыны, және қолданбасыз жою (поштаға код → код).
func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	minutes := int(s.cfg.Auth.OTPTTL.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	messages := map[string]string{}
	for code, key := range map[string]string{
		"INVALID_EMAIL": "delete.error.invalid_email", "INVALID_OTP": "delete.error.invalid_code",
		"OTP_EXPIRED": "delete.error.expired", "OTP_ALREADY_USED": "delete.error.expired",
		"OTP_ATTEMPTS_EXCEEDED": "delete.error.attempts", "RATE_LIMITED": "delete.error.rate_limited",
		"default": "delete.error.generic",
	} {
		messages[code] = s.bundle.T(locale, key)
	}
	boot, err := json.Marshal(map[string]any{"errors": messages})
	if err != nil {
		boot = []byte("{}")
	}
	kept := s.bundle.T(locale, "delete.kept_body_no_expiry")
	if days := s.cfg.Retention.AnonInstallationsDays; days > 0 {
		kept = strings.ReplaceAll(s.bundle.T(locale, "delete.kept_body"), "{after}", wordsFor(locale).period(days))
	}
	s.render(w, "account_delete", "public_layout", accountDeleteView{
		baseView: s.base(locale),
		CodeSent: strings.ReplaceAll(s.bundle.T(locale, "delete.code_sent"), "{minutes}", strconv.Itoa(minutes)),
		KeptBody: kept,
		Boot:     template.JS(boot),
	})
}

// handleDeleteAccountRedirect — /delete-account → /account/delete (тіл сақталады).
func (s *Server) handleDeleteAccountRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/account/delete"
	if lang := r.URL.Query().Get("lang"); lang != "" {
		target += "?lang=" + domain.NormalizeLocale(lang)
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// locale — тілді анықтап, cookie-ге жазады.
func (s *Server) locale(w http.ResponseWriter, r *http.Request) string {
	locale := localization.Detect(r, "en")
	if r.URL.Query().Get("lang") != "" {
		localization.SetCookie(w, locale, s.cfg.App.IsProduction())
	}
	return domain.NormalizeLocale(locale)
}
