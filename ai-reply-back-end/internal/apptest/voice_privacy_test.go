package apptest

import (
	"net/http"
	"strings"
	"testing"
)

// Public policy must disclose the new audio destination and controls in every
// supported locale, independently of the older Apple/Watch dictation paths.
func TestPrivacyPolicyDisclosesIPhoneVoice(t *testing.T) {
	h := newHarness(t)
	for locale, required := range map[string][]string{
		"en": {"AI Reply Voice on iPhone", "explicit permission", "OpenAI (USA)", "Realtime", "30 days", "protected on-device shared storage", "three minutes", "withdraw voice audio consent", "keyboard itself cannot record audio", "without opening the app while a Live Activity remains active", "Tap Done", "40 seconds", "20 seconds", "a short speech pause does not finish"},
		"ru": {"AI Reply Voice на iPhone", "явное разрешение", "OpenAI (США)", "Realtime", "30 дней", "защищённом общем хранилище", "три минуты", "отозвать", "клавиатура не записывает звук", "без открытия приложения при активной Live Activity", "«Готово»", "40 секунд", "20 секунд", "короткая пауза в речи не завершает"},
		"kk": {"AI Reply Voice", "айқын рұқсат", "OpenAI-ға (АҚШ)", "Realtime", "30 күнге", "қорғалған ортақ қоймада", "үш минут", "кері қайтарып", "Пернетақтаның өзі дыбыс жазбайды", "белсенді Live Activity кезінде қолданбаны ашпай", "«Дайын»", "40 секундта", "20 секундта", "қысқа үзіліс сеансты аяқтамайды"},
		"uz": {"AI Reply Voice", "aniq ruxsat", "OpenAI’ga (AQSH)", "Realtime", "30 kungacha", "himoyalangan umumiy xotirada", "uch daqiqa", "qaytarib olish", "Klaviaturaning o‘zi ovoz yozmaydi", "faol Live Activity bilan ilovani ochmasdan", "«Tayyor»", "40 soniyada", "20 soniyada", "qisqa pauza seansni tugatmaydi"},
	} {
		t.Run(locale, func(t *testing.T) {
			res := h.do(http.MethodGet, "/privacy?lang="+locale, nil, nil)
			if res.status != http.StatusOK {
				t.Fatalf("privacy status %d", res.status)
			}
			for _, phrase := range required {
				if !strings.Contains(string(res.raw), phrase) {
					t.Errorf("privacy page omits %q", phrase)
				}
			}
		})
	}
}
