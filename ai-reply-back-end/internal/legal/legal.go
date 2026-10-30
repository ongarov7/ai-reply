package legal

// Versions of the public terms and privacy policy. Bumping either one makes
// every account accept the documents again before AI replies work.
const (
	TermsVersion   = "2026-10-08"
	PrivacyVersion = "2026-10-14"
	UpdatedDate    = "2026-10-14"
)

// AIProvider — жауаптарды жазатын AI провайдері (келісім экраны мен құпиялық саясаты оны атайды).
const AIProvider = "OpenAI"

// WebPurchaseTermsVersion is accepted separately for a website purchase. It
// does not change the app's consent to AI processing or reset existing consent.
const WebPurchaseTermsVersion = "2026-10-30-web-purchase-v1"
