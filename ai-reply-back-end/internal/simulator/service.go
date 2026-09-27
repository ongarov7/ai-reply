// Package simulator — интерактивті өнім демонстрациясының сервистік қабаты.
//
// Симулятор жаңа өнім логикасын ойлап таппайды. Ол бар қызметтерді
// (users, subscriptions, ai, plans) бір бөлек демонстрациялық аккаунтқа
// бағыттайды: сондықтан көрсетілім кезінде жасалған генерация нақты квотаны,
// нақты промптты және нақты провайдерді пайдаланады.
//
// Екі шекара әдейі қатты:
//
//	1. Барлық жазу тек AccountID аккаунтына тиеді. Басқа қолданушының
//	   профилі де, жазылымы да симулятор арқылы өзгермейді.
//	2. Тариф бағасы өндірістік конфигурация. Симулятордағы «баға өзгерту»
//	   тек жадтағы демо қабатқа жазылады (PlanDraft), plans кестесіне емес.
package simulator

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/users"
)

// Демонстрация аккаунты — тұрақты идентификатормен, сондықтан қайта
// жасалмайды және әкімші тізімінде анық көрінеді.
const (
	// AccountID — симулятор аккаунтының тұрақты UUID-і.
	AccountID = "00000000-0000-4000-8000-0000000000d1"
	// AccountEmail — аккаунтты әкімші тізімінде тану үшін.
	AccountEmail = "simulator@demo.aireply.local"
	// AccountKind — users.kind бағанындағы белгі.
	AccountKind = "simulator"
	// AccountLabel — интерфейсте көрсетілетін ат.
	AccountLabel = "Product demo account"
)

// Service — симулятор қабаты.
type Service struct {
	repo   *repository.Store
	users  *users.Service
	subs   *subscriptions.Service
	plans  *plans.Service
	ai     *ai.Service
	limits *limits.Service
	cfg    config.Config
	log    *slog.Logger

	// Демо тариф қабаты: тек жадта, процесс қайта қосылғанда жоғалады.
	mu     sync.RWMutex
	drafts map[string]PlanDraft
}

// Deps — тәуелділіктер.
type Deps struct {
	Repo   *repository.Store
	Users  *users.Service
	Subs   *subscriptions.Service
	Plans  *plans.Service
	AI     *ai.Service
	Limits *limits.Service
	Config config.Config
	Log    *slog.Logger
}

// New — қызмет.
func New(d Deps) *Service {
	return &Service{
		repo: d.Repo, users: d.Users, subs: d.Subs, plans: d.Plans, ai: d.AI, limits: d.Limits,
		cfg: d.Config, log: d.Log, drafts: map[string]PlanDraft{},
	}
}

// ---------------------------------------------------------------- аккаунт

// EnsureAccount — демонстрация аккаунтын тауып береді, болмаса жасайды.
//
// Аккаунт нақты: оның нақты жазылымы, нақты квотасы және нақты usage оқиғалары
// бар. Демонстрация кезіндегі генерация статистикаға да түседі — бұл әдейі,
// себебі «шынымен жұмыс істейді» дегеннің мәні сол.
func (s *Service) EnsureAccount(ctx context.Context) (domain.User, error) {
	user, err := s.repo.UserByID(ctx, AccountID)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrNotFound):
		user, err = s.repo.CreateUser(ctx, domain.User{
			ID:       AccountID,
			Email:    AccountEmail,
			Status:   domain.UserActive,
			Locale:   "ru",
			Timezone: s.cfg.App.Timezone,
			Platform: domain.PlatformWeb,
			Kind:     AccountKind,
		})
		if err != nil && !errors.Is(err, domain.ErrConflict) {
			return domain.User{}, err
		}
		if errors.Is(err, domain.ErrConflict) {
			if user, err = s.repo.UserByID(ctx, AccountID); err != nil {
				return domain.User{}, err
			}
		}
	default:
		return domain.User{}, err
	}

	if err := s.subs.EnsureSubscription(ctx, AccountID); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

// Snapshot — аккаунттың ағымдағы толық күйі.
type Snapshot struct {
	User        domain.User
	Profile     domain.Profile
	Entitlement domain.Entitlement
}

// Account — профиль мен лимиттерді бір оқуда береді.
func (s *Service) Account(ctx context.Context) (Snapshot, error) {
	user, err := s.EnsureAccount(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	profile, err := s.users.Profile(ctx, user.ID)
	if err != nil {
		return Snapshot{}, err
	}
	entitlement, err := s.subs.Entitlement(ctx, user.ID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{User: user, Profile: profile, Entitlement: entitlement}, nil
}

// SaveProfile — демонстрация аккаунтының профилі (нақты users қызметі арқылы).
func (s *Service) SaveProfile(ctx context.Context, in users.ProfileUpdate) (Snapshot, error) {
	if _, err := s.EnsureAccount(ctx); err != nil {
		return Snapshot{}, err
	}
	if _, err := s.users.UpdateProfile(ctx, AccountID, in); err != nil {
		return Snapshot{}, err
	}
	return s.Account(ctx)
}

// AssignPlan — демонстрация аккаунтына тариф беру (нақты жазылым жазбасы).
func (s *Service) AssignPlan(ctx context.Context, planID string) (Snapshot, error) {
	if _, err := s.EnsureAccount(ctx); err != nil {
		return Snapshot{}, err
	}
	plan, err := s.plans.Get(ctx, planID)
	if err != nil {
		return Snapshot{}, err
	}
	var expires *time.Time
	if plan.PeriodDays > 0 {
		until := s.subs.Now().AddDate(0, 0, plan.PeriodDays)
		expires = &until
	}
	if _, err := s.subs.Assign(ctx, AccountID, plan.ID, "simulator", expires); err != nil {
		return Snapshot{}, err
	}
	return s.Account(ctx)
}

// ResetAccount — көрсетілім алдындағы тазарту: квота, профиль, тегін тариф.
func (s *Service) ResetAccount(ctx context.Context) (Snapshot, error) {
	if _, err := s.EnsureAccount(ctx); err != nil {
		return Snapshot{}, err
	}
	date, _ := s.subs.TodayKeys()
	if err := s.repo.ResetDailyQuota(ctx, AccountID, date); err != nil {
		return Snapshot{}, err
	}
	if err := s.repo.SaveProfile(ctx, domain.Profile{UserID: AccountID}); err != nil {
		return Snapshot{}, err
	}
	if free, err := s.plans.Default(ctx); err == nil {
		if _, err := s.subs.Assign(ctx, AccountID, free.ID, "simulator", nil); err != nil {
			return Snapshot{}, err
		}
	}
	return s.Account(ctx)
}

// ResetQuota — тек бүгінгі квотаны нөлдеу (көрсетілім ортасында ыңғайлы).
func (s *Service) ResetQuota(ctx context.Context) (Snapshot, error) {
	if _, err := s.EnsureAccount(ctx); err != nil {
		return Snapshot{}, err
	}
	date, _ := s.subs.TodayKeys()
	if err := s.repo.ResetDailyQuota(ctx, AccountID, date); err != nil {
		return Snapshot{}, err
	}
	return s.Account(ctx)
}

// ---------------------------------------------------------------- генерация

// Override — бір ғана сұранысқа арналған профиль ауыстырғышы.
//
// Бұл «бір сұрақ, екі бизнес — екі түрлі жауап» демонстрациясы үшін керек:
// сақталған профиль өзгермейді, тек осы генерация басқа контекстпен жүреді.
type Override struct {
	Description      string
	Role             string
	Tone             string
	BusinessOffering string
	BusinessSummary  string
	BusinessRules    []string
}

// GenerateInput — симулятордан келетін сұраныс.
type GenerateInput struct {
	SourceText  string
	Instruction string
	Language    string
	TemplateID  string
	Platform    string
	Override    *Override
}

// GenerateResult — нәтиже және техникалық көрсетілімге жарайтын метадерек.
type GenerateResult struct {
	Reply            string
	DetectedLanguage string
	Model            string
	InputTokens      int
	OutputTokens     int
	LatencyMS        int
	Entitlement      domain.Entitlement
	// Trace — сұйылтылған із: промптқа қандай блоктар кіргені, құпиясыз.
	Trace []string
}

// LimitInfo — квота таусылғанда клиентке қайтатын мәлімет.
type LimitInfo struct {
	DailyLimit int
	UsedToday  int
	ResetsAt   time.Time
}

// Generate — нақты AI шлюзі арқылы жауап алу.
//
// Мәтін ешқайда жазылмайды: ai.Service тек метадерек сақтайды, ал бұл қабат
// одан да азын біледі.
func (s *Service) Generate(ctx context.Context, in GenerateInput) (GenerateResult, LimitInfo, error) {
	user, err := s.EnsureAccount(ctx)
	if err != nil {
		return GenerateResult{}, LimitInfo{}, err
	}
	profile, err := s.users.Profile(ctx, user.ID)
	if err != nil {
		return GenerateResult{}, LimitInfo{}, err
	}

	prompt := ai.Profile{
		Description:   profile.Description,
		Role:          profile.Role,
		PreferredTone: profile.PreferredTone,
		Business: ai.Business{
			Offering: profile.BusinessOffering,
			Summary:  profile.BusinessSummary,
			Rules:    profile.BusinessRules,
		},
	}
	trace := []string{"incoming_message"}
	if strings.TrimSpace(in.Instruction) != "" {
		trace = append(trace, "user_instruction")
	}
	if in.Override != nil {
		applyOverride(&prompt, *in.Override)
		trace = append(trace, "profile_override")
	}
	if prompt.Description != "" || prompt.Role != "" {
		trace = append(trace, "user_profile")
	}
	if !prompt.Business.IsEmpty() {
		trace = append(trace, "business_context")
	}
	if len(prompt.Business.Rules) > 0 {
		trace = append(trace, "user_rules")
	}

	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	if !traits.OneOf(platform, domain.PlatformIOS, domain.PlatformAndroid) {
		platform = domain.PlatformWeb
	}

	result, genErr := s.ai.Reply(ctx, ai.Request{
		User:        user,
		SourceText:  in.SourceText,
		Instruction: in.Instruction,
		Language:    in.Language,
		TemplateID:  in.TemplateID,
		Profile:     prompt,
		Template:    template(in.TemplateID, prompt.PreferredTone),
		Platform:    platform,
		AppVersion:  "simulator",
	})
	if genErr != nil {
		return GenerateResult{}, LimitInfo{
			DailyLimit: result.DailyLimit, UsedToday: result.UsedToday, ResetsAt: result.ResetsAt,
		}, genErr
	}

	entitlement, err := s.subs.Entitlement(ctx, user.ID)
	if err != nil {
		return GenerateResult{}, LimitInfo{}, err
	}
	return GenerateResult{
		Reply:            result.Text,
		DetectedLanguage: result.DetectedLanguage,
		Model:            result.Model,
		InputTokens:      result.InputTokens,
		OutputTokens:     result.OutputTokens,
		LatencyMS:        result.LatencyMS,
		Entitlement:      entitlement,
		Trace:            trace,
	}, LimitInfo{}, nil
}

func applyOverride(p *ai.Profile, o Override) {
	if o.Description != "" {
		p.Description = traits.Clamp(o.Description, 1000)
	}
	if o.Role != "" {
		p.Role = traits.Clamp(o.Role, 120)
	}
	if traits.OneOf(o.Tone, "natural", "friendly", "professional", "formal", "short") {
		p.PreferredTone = o.Tone
	}
	business := ai.Business{
		Offering: traits.Clamp(o.BusinessOffering, 120),
		Summary:  traits.Clamp(o.BusinessSummary, 400),
	}
	for _, rule := range o.BusinessRules {
		if rule = traits.Clamp(rule, 200); rule != "" {
			business.Rules = append(business.Rules, rule)
		}
		if len(business.Rules) == 8 {
			break
		}
	}
	if !business.IsEmpty() {
		p.Business = business
	}
}

// template — симулятордағы қарым-қатынас түрін мобильді шаблонға айналдыру.
func template(id, fallbackTone string) ai.Template {
	tone := fallbackTone
	if tone == "" {
		tone = "natural"
	}
	switch id {
	case "client", "business", "work", "friend":
	default:
		id = "client"
	}
	if id == "friend" {
		return ai.Template{Relationship: id, Tone: "friendly", ReplyLength: "short",
			EmojiPolicy: "allowed", WorkingHoursBehavior: "ignore"}
	}
	return ai.Template{Relationship: id, Tone: tone, ReplyLength: "short",
		EmojiPolicy: "minimal", WorkingHoursBehavior: "mention_when_relevant"}
}

// ---------------------------------------------------------------- тарифтер

// Plans — нақты тариф каталогы (тек оқу).
func (s *Service) Plans(ctx context.Context) ([]domain.Plan, error) { return s.plans.All(ctx) }

// PlanDraft — демо қабаттағы тариф параметрлері. plans кестесіне жазылмайды.
type PlanDraft struct {
	PlanID       string
	Price        int64
	DailyLimit   int
	MonthlyLimit int
	PeriodDays   int
	UpdatedAt    time.Time
}

// Drafts — ағымдағы демо өзгерістері.
func (s *Service) Drafts() map[string]PlanDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]PlanDraft, len(s.drafts))
	for k, v := range s.drafts {
		out[k] = v
	}
	return out
}

// SaveDraft — демо тариф өзгерісі. Нақты бағаға әсері жоқ.
func (s *Service) SaveDraft(ctx context.Context, d PlanDraft) (PlanDraft, error) {
	plan, err := s.plans.Get(ctx, d.PlanID)
	if err != nil {
		return PlanDraft{}, err
	}
	if d.Price < 0 || d.DailyLimit < 0 || d.MonthlyLimit < 0 || d.PeriodDays < 0 {
		return PlanDraft{}, domain.ErrInvalidRequest
	}
	if d.DailyLimit > 10000 || d.MonthlyLimit > 1000000 || d.PeriodDays > 3650 || d.Price > 100000000 {
		return PlanDraft{}, domain.ErrInvalidRequest
	}
	d.PlanID = plan.ID
	d.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	s.drafts[plan.ID] = d
	s.mu.Unlock()
	return d, nil
}

// ClearDrafts — демо тарифтерді өндірістік мәндерге қайтару.
func (s *Service) ClearDrafts() {
	s.mu.Lock()
	s.drafts = map[string]PlanDraft{}
	s.mu.Unlock()
}

// ---------------------------------------------------------------- күй

// Health — көрсетілім алдында бэкендтің шын күйі.
type Health struct {
	Database           bool
	ProviderConfigured bool
	Model              string
	Env                string
	Timezone           string
	PaymentMode        string
	DemoAuth           bool
	LegacyAPI          bool
	SourceTextChars    int
	InstructionChars   int
	AIPerMinute        int
	CheckedAt          time.Time
}

// Health — нақты тексеру: дерекқорға ping, провайдер кілтінің бар-жоғы.
//
// Кілттің өзі ешқашан қайтарылмайды — тек «бапталған ба» деген жауап.
func (s *Service) Health(ctx context.Context) Health {
	dbOK := true
	if _, err := s.plans.Active(ctx); err != nil {
		dbOK = false
	}
	key := strings.TrimSpace(s.cfg.OpenAI.APIKey)
	current := s.limits.Current(ctx)
	return Health{
		Database:           dbOK,
		ProviderConfigured: key != "" && !strings.Contains(key, "REPLACE"),
		Model:              s.cfg.OpenAI.Model,
		Env:                s.cfg.App.Env,
		Timezone:           s.cfg.App.Timezone,
		PaymentMode:        s.cfg.Payments.Mode,
		DemoAuth:           s.cfg.Auth.DemoMode,
		LegacyAPI:          s.cfg.Auth.LegacyEnabled,
		SourceTextChars:    current.SourceChars,
		InstructionChars:   current.InstructionChars,
		AIPerMinute:        s.cfg.Limits.AIPerMinute,
		CheckedAt:          time.Now().UTC(),
	}
}

// Timezone — квота белдеуі.
func (s *Service) Timezone() string { return s.cfg.App.Timezone }
