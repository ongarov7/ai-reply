package admin

import "strings"

// iPhoneNames — Apple-дың модель идентификаторы → сатылымдағы атауы.
//
// Only the identifiers that exist for the iOS versions this app supports
// (iOS 17+). An identifier missing here is shown as is: the raw identifier
// is always kept for debugging, the friendly name is a convenience.
var iPhoneNames = map[string]string{
	"iPhone11,2": "iPhone XS", "iPhone11,4": "iPhone XS Max", "iPhone11,6": "iPhone XS Max", "iPhone11,8": "iPhone XR",
	"iPhone12,1": "iPhone 11", "iPhone12,3": "iPhone 11 Pro", "iPhone12,5": "iPhone 11 Pro Max",
	"iPhone12,8": "iPhone SE (2nd generation)",
	"iPhone13,1": "iPhone 12 mini", "iPhone13,2": "iPhone 12", "iPhone13,3": "iPhone 12 Pro", "iPhone13,4": "iPhone 12 Pro Max",
	"iPhone14,4": "iPhone 13 mini", "iPhone14,5": "iPhone 13", "iPhone14,2": "iPhone 13 Pro", "iPhone14,3": "iPhone 13 Pro Max",
	"iPhone14,6": "iPhone SE (3rd generation)",
	"iPhone14,7": "iPhone 14", "iPhone14,8": "iPhone 14 Plus", "iPhone15,2": "iPhone 14 Pro", "iPhone15,3": "iPhone 14 Pro Max",
	"iPhone15,4": "iPhone 15", "iPhone15,5": "iPhone 15 Plus", "iPhone16,1": "iPhone 15 Pro", "iPhone16,2": "iPhone 15 Pro Max",
	"iPhone17,3": "iPhone 16", "iPhone17,4": "iPhone 16 Plus", "iPhone17,1": "iPhone 16 Pro", "iPhone17,2": "iPhone 16 Pro Max",
	"iPhone17,5": "iPhone 16e",
}

// DeviceName — әкімші панеліне арналған түсінікті атау.
func DeviceName(platform, manufacturer, model string) string {
	model = strings.TrimSpace(model)
	switch {
	case model == "":
		return ""
	case platform == "ios":
		if name, ok := iPhoneNames[model]; ok {
			return name
		}
		if model == "arm64" || model == "x86_64" {
			return "iOS Simulator"
		}
		return model
	default:
		m := strings.TrimSpace(manufacturer)
		if m == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(m)) {
			return model
		}
		return strings.ToUpper(m[:1]) + m[1:] + " " + model
	}
}
