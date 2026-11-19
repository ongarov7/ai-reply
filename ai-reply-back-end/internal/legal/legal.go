package legal

// Versions of the public terms and privacy policy. Bumping either one makes
// every account accept the documents again before AI replies work.
const (
	TermsVersion   = "2026-10-08"
	PrivacyVersion = "2026-10-14"
	// The edition dates shown on the pages. A wording change that needs no
	// new acceptance moves only these.
	TermsUpdatedDate   = "2026-11-19"
	PrivacyUpdatedDate = "2026-11-19"
)

// AIProvider — жауаптарды жазатын AI провайдері (келісім экраны мен құпиялық саясаты оны атайды).
const AIProvider = "OpenAI"

// ScreenshotConsentVersion — the in-app disclosure a person accepts before a
// screenshot itself (not only its recognised text) is sent for analysis. A
// screenshot reply in image mode must carry it; changing what image analysis
// does changes this version and asks again, without touching the general AI
// consent.
const ScreenshotConsentVersion = "2026-11-19-screenshot-v1"

// WebPurchaseTermsVersion is accepted separately for a website purchase. It
// does not change the app's consent to AI processing or reset existing consent.
const WebPurchaseTermsVersion = "2026-10-30-web-purchase-v1"
