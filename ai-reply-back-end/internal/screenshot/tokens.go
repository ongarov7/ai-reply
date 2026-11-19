package screenshot

import "unicode"

// EstimateTokens — a deliberately generous token count for text, used only
// when the provider cannot count a request itself. It assumes about 3.6
// characters per token for Latin text, spaces and punctuation, 3 for other
// letters (Cyrillic, Kazakh) and a little over one token for every emoji or
// other symbol, so a fallback estimate errs towards asking the person to
// approve a little more, never towards charging more than they approved: the
// final charge always comes from the provider's own usage report.
func EstimateTokens(s string) int {
	var latin, letters, symbols float64
	for _, r := range s {
		switch {
		case r < 0x80:
			latin++
		case unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r):
			letters++
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			latin++
		default:
			symbols++
		}
	}
	if latin+letters+symbols == 0 {
		return 0
	}
	return int(latin/3.6+letters/3+symbols*1.2) + 1
}
