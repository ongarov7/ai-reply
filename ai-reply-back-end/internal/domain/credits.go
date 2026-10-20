package domain

import "time"

// Генерацияны төлейтін бөліктер (usage.bucket, usage_reservations.bucket).
//
// The order is the consumption order: the plan's daily allowance first, then
// an active pass, the welcome bonus, admin grants and purchased packs
// (oldest first). A monthly plan cap only closes the daily bucket.
const (
	BucketDaily   = "daily"
	BucketPass    = "pass"
	BucketWelcome = "welcome"
	BucketAdmin   = "admin"
	BucketPack    = "pack"
)

// Кредит түрлері (credit_grants.kind). Атаулары бөліктердің атауымен бірдей.
const (
	GrantWelcome = BucketWelcome
	GrantAdmin   = BucketAdmin
	GrantPack    = BucketPack
	GrantPass    = BucketPass
)

// GrantKinds — тұтыну ретімен.
var GrantKinds = []string{GrantPass, GrantWelcome, GrantAdmin, GrantPack}

// Кредит күйлері (credit_grants.status).
const (
	GrantActive  = "active"
	GrantRevoked = "revoked"
)

// Журнал жазбалары (credit_ledger.entry).
const (
	LedgerGrant   = "grant"
	LedgerConsume = "consume"
	LedgerRefund  = "refund"
	LedgerRevoke  = "revoke"
)

// LedgerEntries — журнал жазбаларының барлық түрі.
var LedgerEntries = []string{LedgerGrant, LedgerConsume, LedgerRefund, LedgerRevoke}

// Брон күйлері (usage_reservations.status).
const (
	ReservationReserved  = "reserved"
	ReservationCommitted = "committed"
	ReservationRefunded  = "refunded"
)

// Idempotency-Key қайталауы.
//
// A key that already paid for a generation is never charged again. Within
// IdempotencyReplayWindow of the charge it may generate IdempotencyMaxReplays
// more time(s) for free; the text itself is never stored to serve a replay.
const (
	IdempotencyReplayWindow = 10 * time.Minute
	IdempotencyMaxReplays   = 1
)

// CreditGrant — тарифтен тыс берілген генерациялар (бонус, әкімші, пакет, pass).
type CreditGrant struct {
	ID             string
	UserID         string
	Kind           string
	UnitsTotal     int
	UnitsRemaining int
	StartsAt       time.Time
	// ExpiresAt — nil: мерзімсіз (бонус, әкімші, пакет).
	ExpiresAt   *time.Time
	Status      string
	ProductCode string
	// GrantedBy — "system" не admin_users.id.
	GrantedBy string
	// Reason — әкімшінің қолмен берген кредитке жазған түсініктемесі.
	Reason       string
	RevokedAt    *time.Time
	RevokedBy    string
	RevokeReason string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LedgerEntry — кредит журналының бір жазбасы (мазмұнсыз).
type LedgerEntry struct {
	ID      string
	UserID  string
	GrantID string
	Entry   string
	// Kind — кредиттің түрі: welcome | admin | pack | pass.
	Kind  string
	Delta int
	// Actor — "system" не admin_users.id.
	Actor     string
	Reason    string
	CreatedAt time.Time
}

// CreditBalance — тарифтен тыс қалған генерациялар.
type CreditBalance struct {
	// HasWelcome — тіркелгіде сәлемдесу бонусы бар (қайтарылғаны да).
	HasWelcome       bool
	WelcomeTotal     int
	WelcomeRemaining int
	// Admin, Packs — әкімші берген және сатып алынған қалдық.
	Admin int
	Packs int
	// PassActive — қазір жарамды pass бар (бірліктері біткен болса да).
	PassActive    bool
	PassRemaining int
	PassExpiresAt *time.Time
}

// Credits — әкімші кредиттері мен пакеттердің қалдығы (usage.credits).
func (b CreditBalance) Credits() int { return b.Admin + b.Packs }

// Extra — тарифтен тыс жұмсауға болатынның бәрі: pass + бонус + кредиттер.
func (b CreditBalance) Extra() int { return b.PassRemaining + b.WelcomeRemaining + b.Credits() }

// Spent — бос қалған баланс (лимитке тірелгендегі күй): уақыт белгілері қалады.
func (b CreditBalance) Spent() CreditBalance {
	b.WelcomeRemaining, b.Admin, b.Packs, b.PassRemaining = 0, 0, 0, 0
	return b
}
