package domain

import (
	"errors"
	"time"
)

// Дүкендер (iap_purchases.provider, iap_notifications.provider).
const (
	ProviderAppStore   = "app_store"
	ProviderGooglePlay = "google_play"
)

// IAPProviders — сүзгі мен әкімші беті үшін.
var IAPProviders = []string{ProviderAppStore, ProviderGooglePlay}

// Өнім түрлері (iap_products.kind): пакет мерзімсіз, pass — мерзімді.
const (
	ProductPack         = GrantPack
	ProductPass         = GrantPass
	ProductSubscription = "subscription"
)

// IAPProductKinds — рұқсат етілген түрлер.
var IAPProductKinds = []string{ProductPack, ProductPass, ProductSubscription}

// Сатып алу күйлері (iap_purchases.status).
//
// pending: Google says the payment is still pending (cash, bank transfer);
// nothing is granted. granted: verified and the units are on the account.
// refunded / revoked: the store took it back (refund, Family Sharing, voided);
// what was left on the grant is gone. rejected: a pending purchase the store
// cancelled, never paid.
const (
	PurchasePending  = "pending"
	PurchaseGranted  = "granted"
	PurchaseRefunded = "refunded"
	PurchaseRevoked  = "revoked"
	PurchaseRejected = "rejected"
)

// IAPPurchaseStatuses — барлық күй (әкімші сүзгісі осы ретпен).
var IAPPurchaseStatuses = []string{PurchasePending, PurchaseGranted, PurchaseRefunded, PurchaseRevoked, PurchaseRejected}

// Дүкен орталары (iap_purchases.environment). Тек Production ақшалы сатылым.
const (
	EnvironmentProduction = "Production"
	EnvironmentSandbox    = "Sandbox" // App Store: TestFlight, App Review, sandbox testers
	EnvironmentTest       = "Test"    // Google Play: license testers (purchaseType 0)
	EnvironmentPromo      = "Promo"   // Google Play: promo codes (purchaseType 1)
)

// IAPEnvironments — сүзгі үшін.
var IAPEnvironments = []string{EnvironmentProduction, EnvironmentSandbox, EnvironmentTest, EnvironmentPromo}

// PURCHASE_INVALID себептері (details.reason). Клиент оларды көрсетпейді, тек журналға жазады.
const (
	PurchaseReasonMalformed    = "malformed"        // JWS / token is not what a store sends
	PurchaseReasonSignature    = "signature"        // the JWS signature or certificate chain does not verify
	PurchaseReasonBundle       = "bundle_mismatch"  // another app's transaction
	PurchaseReasonEnvironment  = "environment"      // Sandbox / a Google license test on a server that does not accept it
	PurchaseReasonProduct      = "product_unknown"  // no product with this store id, or Google names another product
	PurchaseReasonAccount      = "account_mismatch" // appAccountToken / obfuscatedAccountId is not this account's
	PurchaseReasonRevoked      = "revoked"          // refunded or revoked by the store
	PurchaseReasonType         = "product_type"     // not a consumable / non-renewing subscription
	PurchaseReasonCanceled     = "canceled"         // Google: purchaseState 1
	PurchaseReasonNotFound     = "not_found"        // Google does not know the token
	PurchaseReasonConsumed     = "already_consumed" // Google: consumed, but not by this server
	PurchaseReasonQuantity     = "quantity"         // quantity is not a positive number
	PurchaseReasonPurchaseType = "unsupported_type" // Google: rewarded product
)

// IAP қателері.
var (
	// ErrIAPUnavailable — бұл платформада дүкен интеграциясы бапталмаған не сатылым өшірулі (403).
	ErrIAPUnavailable = errors.New("in-app purchases are not available")
	// ErrPurchaseInvalid — дүкен растамады (400 PURCHASE_INVALID, себебі — *PurchaseError).
	ErrPurchaseInvalid = errors.New("purchase is not valid")
	// ErrPurchaseOwnedElsewhere — бұл транзакция басқа тіркелгіге берілген (409).
	ErrPurchaseOwnedElsewhere = errors.New("purchase belongs to another account")
	// ErrStoreUnavailable — дүкен API-і уақытша жауап бермеді: клиент кейін қайта жібереді (503).
	ErrStoreUnavailable = errors.New("store verification is temporarily unavailable")
)

// PurchaseError — дүкен растамаған сатып алу және себебі.
type PurchaseError struct {
	Reason string
}

func (e *PurchaseError) Error() string { return ErrPurchaseInvalid.Error() + ": " + e.Reason }

// Unwrap — ErrPurchaseInvalid.
func (e *PurchaseError) Unwrap() error { return ErrPurchaseInvalid }

// Details — клиентке себеп (мән емес).
func (e *PurchaseError) Details() map[string]any { return map[string]any{"reason": e.Reason} }

// PurchaseInvalid — PurchaseError жасайды.
func PurchaseInvalid(reason string) error { return &PurchaseError{Reason: reason} }

// IAPProduct — әкімші баптаған өнім: дүкендегі идентификаторлар және не береді.
//
// Prices live in App Store Connect and Play Console only; the apps show the
// store's localized price. A product is offered while it is active, not
// archived, has units and the platform's store id.
type IAPProduct struct {
	ID              string
	Code            string
	Kind            string
	Units           int
	DurationHours   int
	PlanID          string
	BillingPeriod   string
	PriceMinor      int64
	Currency        string
	AppleProductID  string
	GoogleProductID string
	Name            map[string]string
	IsActive        bool
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ArchivedAt      *time.Time
}

// StoreID — платформаның дүкендегі идентификаторы.
func (p IAPProduct) StoreID(platform string) string {
	switch platform {
	case PlatformIOS:
		return p.AppleProductID
	case PlatformAndroid:
		return p.GoogleProductID
	}
	return ""
}

// Offered — бұл платформада сатылады ма.
func (p IAPProduct) Offered(platform string) bool {
	valid := p.Units > 0
	if p.Kind == ProductSubscription {
		valid = p.PlanID != "" && (p.BillingPeriod == "month" || p.BillingPeriod == "year") && platform == PlatformIOS
	}
	return p.IsActive && p.ArchivedAt == nil && valid && p.StoreID(platform) != ""
}

// LocalizedName — тілдегі атау, болмаса ru → en → код.
func (p IAPProduct) LocalizedName(locale string) string {
	for _, l := range []string{NormalizeLocale(locale), "ru", "en", "kk", "uz"} {
		if v := p.Name[l]; v != "" {
			return v
		}
	}
	return p.Code
}

// IAPPurchase — бір дүкен транзакциясы (мазмұнсыз).
//
// TransactionID is Apple's transactionId, or the SHA-256 hex of a Google
// purchase token: the raw token is never stored in the clear.
type IAPPurchase struct {
	ID string
	// UserID — "" after the account was deleted (the record is kept, the person is not).
	UserID                string
	Provider              string
	Environment           string
	StoreProductID        string
	ProductCode           string
	TransactionID         string
	OriginalTransactionID string
	OrderID               string
	Quantity              int
	Units                 int
	PriceMilliunits       int64
	Currency              string
	Status                string
	ConsumedAt            *time.Time
	PurchasedAt           *time.Time
	RevokedAt             *time.Time
	RevokeReason          string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// IAPNotification — дүкен хабарламасы (тек түрі мен нәтижесі).
type IAPNotification struct {
	Provider       string
	NotificationID string
	Type           string
	Subtype        string
	TransactionID  string
	Outcome        string
	ReceivedAt     time.Time
}
