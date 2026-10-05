package simulator

import "time"

// Бұл файлдағының бәрі — ойдан шығарылған демонстрациялық дерек.
//
// Әкімші бөлімі көрсетілімде нақты қолданушыларды ЕШҚАШАН көрсетпейді:
// клиентке панельдің қалай жұмыс істейтінін көрсету үшін нақты адамның
// нөмірі де, қолданысы да қажет емес. Сондықтан тізім осында, кодта тұр.
//
// Нақты деректер симуляторда тек екі жерде: тариф каталогы (тек оқу) және
// демонстрация аккаунтының өз квотасы.

// DemoUser — әкімші тізіміндегі ойдан шығарылған жол.
type DemoUser struct {
	ID          string
	Label       string
	Identifier  string
	PlanCode    string
	SubStatus   string
	UsedToday   int
	DailyLimit  int
	TokensMonth int
	Platform    string
	AppVersion  string
	Locale      string
	Tone        string
	Business    string
	Registered  string
	LastActive  string
	Status      string
	Events      []DemoEvent
}

// DemoEvent — қолданушының соңғы сұраныстары (мәтінсіз, тек метадерек).
type DemoEvent struct {
	At        string
	Status    string
	Platform  string
	Language  string
	Tokens    int
	LatencyMS int
	ErrorCode string
}

// DemoSeries — графикке арналған нүкте.
type DemoSeries struct {
	Label string
	Value float64
}

// DemoUsers — көрсетілімге арналған тізім.
func DemoUsers() []DemoUser {
	return []DemoUser{
		{
			ID: "d1a2", Label: "Aigerim S.", Identifier: "+7 701 •••• 42", PlanCode: "pro",
			SubStatus: "active", UsedToday: 31, DailyLimit: 50, TokensMonth: 184_200,
			Platform: "ios", AppVersion: "1.4.2", Locale: "kk", Tone: "friendly",
			Business:   "Cosmetics store, delivery across Kazakhstan",
			Registered: "2026-02-11", LastActive: "2026-09-21 10:42", Status: "active",
			Events: []DemoEvent{
				{At: "10:42", Status: "success", Platform: "ios", Language: "kk", Tokens: 168, LatencyMS: 940},
				{At: "10:31", Status: "success", Platform: "ios", Language: "ru", Tokens: 191, LatencyMS: 1120},
				{At: "09:58", Status: "success", Platform: "ios", Language: "kk", Tokens: 142, LatencyMS: 880},
			},
		},
		{
			ID: "d2b7", Label: "Daniyar T.", Identifier: "+7 777 •••• 08", PlanCode: "standard",
			SubStatus: "active", UsedToday: 12, DailyLimit: 30, TokensMonth: 71_400,
			Platform: "android", AppVersion: "1.4.0", Locale: "ru", Tone: "professional",
			Business:   "Auto parts, wholesale and retail",
			Registered: "2026-04-03", LastActive: "2026-09-21 09:15", Status: "active",
			Events: []DemoEvent{
				{At: "09:15", Status: "success", Platform: "android", Language: "ru", Tokens: 205, LatencyMS: 1310},
				{At: "08:47", Status: "error", Platform: "android", Language: "ru", ErrorCode: "AI_TIMEOUT", LatencyMS: 20_000},
			},
		},
		{
			ID: "d3c1", Label: "Madina K.", Identifier: "+7 705 •••• 77", PlanCode: "free",
			SubStatus: "active", UsedToday: 7, DailyLimit: 7, TokensMonth: 9_800,
			Platform: "ios", AppVersion: "1.4.2", Locale: "ru", Tone: "natural",
			Business:   "Handmade jewellery, Instagram orders",
			Registered: "2026-09-02", LastActive: "2026-09-21 08:03", Status: "active",
			Events: []DemoEvent{
				{At: "08:03", Status: "error", Platform: "ios", Language: "ru", ErrorCode: "DAILY_LIMIT_REACHED"},
				{At: "07:51", Status: "success", Platform: "ios", Language: "ru", Tokens: 158, LatencyMS: 1010},
			},
		},
		{
			ID: "d4e9", Label: "Olim R.", Identifier: "+998 90 •••• 31", PlanCode: "standard",
			SubStatus: "payment_pending", UsedToday: 4, DailyLimit: 30, TokensMonth: 33_150,
			Platform: "android", AppVersion: "1.3.9", Locale: "uz", Tone: "professional",
			Business:   "Language school, group enrolment",
			Registered: "2026-06-19", LastActive: "2026-09-20 19:27", Status: "active",
			Events: []DemoEvent{
				{At: "19:27", Status: "success", Platform: "android", Language: "uz", Tokens: 176, LatencyMS: 1180},
			},
		},
		{
			ID: "d5f4", Label: "Sergey B.", Identifier: "+7 702 •••• 19", PlanCode: "pro",
			SubStatus: "expired", UsedToday: 0, DailyLimit: 7, TokensMonth: 2_400,
			Platform: "ios", AppVersion: "1.2.7", Locale: "ru", Tone: "formal",
			Business:   "Legal consulting for SMEs",
			Registered: "2026-01-08", LastActive: "2026-09-12 14:05", Status: "disabled",
			Events: []DemoEvent{
				{At: "14:05", Status: "error", Platform: "ios", Language: "ru", ErrorCode: "SUBSCRIPTION_EXPIRED"},
			},
		},
	}
}

// DemoMetrics — панельдің жоғарғы көрсеткіштері (ойдан шығарылған).
type DemoMetrics struct {
	TotalUsers      int
	NewToday        int
	ActiveMonth     int
	RequestsToday   int
	SuccessRate     float64
	AverageLatency  int
	TokensMonth     int
	EstimatedCost   float64
	PaidUsers       int
	FreeUsers       int
	IOSUsers        int
	AndroidUsers    int
	Generations     []DemoSeries
	Registrations   []DemoSeries
	PlanMix         []DemoSeries
	PlatformMix     []DemoSeries
	LanguageMix     []DemoSeries
	GeneratedAtNote string
}

// Metrics — көрсетілім үшін тұрақты (кездейсоқ емес) сандар.
func Metrics() DemoMetrics {
	days := []float64{182, 214, 196, 248, 271, 233, 305, 288, 324, 341, 318, 366, 392, 374}
	regs := []float64{9, 14, 11, 18, 22, 16, 25, 19, 27, 31, 24, 29, 35, 33}
	labels := seriesLabels(len(days))

	generations := make([]DemoSeries, 0, len(days))
	registrations := make([]DemoSeries, 0, len(regs))
	for i := range days {
		generations = append(generations, DemoSeries{Label: labels[i], Value: days[i]})
		registrations = append(registrations, DemoSeries{Label: labels[i], Value: regs[i]})
	}

	return DemoMetrics{
		TotalUsers: 1_284, NewToday: 33, ActiveMonth: 612, RequestsToday: 374,
		SuccessRate: 98.4, AverageLatency: 1_120, TokensMonth: 4_182_600, EstimatedCost: 41.38,
		PaidUsers: 318, FreeUsers: 966, IOSUsers: 742, AndroidUsers: 542,
		Generations:   generations,
		Registrations: registrations,
		PlanMix: []DemoSeries{
			{Label: "free", Value: 966}, {Label: "standard", Value: 214}, {Label: "pro", Value: 104},
		},
		PlatformMix: []DemoSeries{
			{Label: "ios", Value: 742}, {Label: "android", Value: 542},
		},
		LanguageMix: []DemoSeries{
			{Label: "ru", Value: 641}, {Label: "kk", Value: 402},
			{Label: "uz", Value: 158}, {Label: "en", Value: 83},
		},
		GeneratedAtNote: "demo",
	}
}

// seriesLabels — соңғы n күннің күні (бүгінге дейін).
func seriesLabels(n int) []string {
	out := make([]string, 0, n)
	today := time.Now().UTC()
	for i := n - 1; i >= 0; i-- {
		out = append(out, today.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	return out
}
