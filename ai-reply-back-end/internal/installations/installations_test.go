package installations

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/push"
)

func testService() *Service {
	return New(nil, "test-access-secret-that-is-long-enough-000", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSealerRoundTripAndTamperDetection(t *testing.T) {
	s := NewSealer("secret-one-that-is-long-enough-000000")
	token := strings.Repeat("ab", 32)
	sealed, err := s.Seal(token)
	if err != nil || strings.Contains(sealed, token) || !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("sealed = %q, %v", sealed, err)
	}
	again, _ := s.Seal(token)
	if again == sealed {
		t.Fatal("each seal must use a fresh nonce")
	}
	if opened, err := s.Open(sealed); err != nil || opened != token {
		t.Fatalf("open = %q, %v", opened, err)
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := s.Open(tampered); !errors.Is(err, ErrUnseal) {
		t.Fatalf("tampered value opened: %v", err)
	}
	if _, err := NewSealer("another-secret-that-is-long-enough-0").Open(sealed); !errors.Is(err, ErrUnseal) {
		t.Fatal("a rotated secret must not open old tokens")
	}
}

func TestRegistrationValidation(t *testing.T) {
	svc := testService()
	apns := strings.Repeat("AB", 32)
	fcm := "dGVzdF9mY21fdG9rZW4:APA91bHPRgkF3JUikC4ENAHEeMrd41Zxv3hVZjC9KtT8OvPVGJ-hQMRKRrZuJAEcl7B338qju59zJMjw2DeAQ"
	in, err := svc.normalize(Registration{
		InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "iOS", AppVersion: "1.3.2",
		AppBuild: "142", OSVersion: "26.0", DeviceModel: "iPhone17,1", Locale: "kk-KZ",
		Timezone: "Asia/Almaty", Permission: "authorized",
		Push: &PushToken{Token: apns, Environment: "development"},
	})
	if err != nil {
		t.Fatalf("valid registration refused: %v", err)
	}
	if in.Platform != "ios" || in.Provider != "apns" || in.Environment != "sandbox" || in.Locale != "kk" ||
		in.TokenHash != push.TokenHash(strings.ToLower(apns)) || strings.Contains(in.TokenSealed, apns) ||
		in.OSName != "iOS" || !in.NotificationsEnabled {
		t.Fatalf("normalised = %+v", in)
	}
	if _, err := svc.normalize(Registration{InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e",
		Platform: "android", Push: &PushToken{Token: fcm}}); err != nil {
		t.Fatalf("fcm token refused: %v", err)
	}
	for name, reg := range map[string]Registration{
		"no id":        {Platform: "ios"},
		"short id":     {InstallationID: "abc", Platform: "ios"},
		"id injection": {InstallationID: "0f8fad5b' OR 1=1 --", Platform: "ios"},
		"platform":     {InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "web"},
		"permission":   {InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "ios", Permission: "maybe"},
		"wrong provider": {InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "ios",
			Push: &PushToken{Provider: "fcm", Token: fcm}},
		"apns not hex": {InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "ios",
			Push: &PushToken{Token: strings.Repeat("zz", 32)}},
		"fcm too short": {InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "android",
			Push: &PushToken{Token: "short"}},
		"environment": {InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "ios",
			Push: &PushToken{Token: apns, Environment: "staging"}},
	} {
		_, err := svc.normalize(reg)
		var field *domain.FieldError
		if err == nil || !errors.As(err, &field) || !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMetadataIsCleanedNotTrusted(t *testing.T) {
	svc := testService()
	in, err := svc.normalize(Registration{
		InstallationID: "0f8fad5b-d9cb-469f-a165-70867728950e", Platform: "android",
		DeviceModel: "SM-S928B<script>", Manufacturer: "samsung", AppVersion: strings.Repeat("9", 80),
		Locale: "<ru>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.DeviceModel != "" || in.Manufacturer != "samsung" || len(in.AppVersion) != 32 || in.Locale != "" {
		t.Fatalf("metadata = %+v", in)
	}
}
