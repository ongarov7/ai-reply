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

const testInstallationID = "0f8fad5b-d9cb-469f-a165-70867728950e"

func TestSealerRoundTripAndTamperDetection(t *testing.T) {
	s := NewSealer("secret-one-that-is-long-enough-000000")
	token := "dGVzdF9mY21fdG9rZW4:APA91bHPRgkF3JUikC4ENAHEeMrd41Zxv3hVZjC9KtT8"
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

// Екі платформа да FCM токенін тіркейді.
func TestRegistrationValidation(t *testing.T) {
	svc := testService()
	fcm := "dGVzdF9mY21fdG9rZW4:APA91bHPRgkF3JUikC4ENAHEeMrd41Zxv3hVZjC9KtT8OvPVGJ-hQMRKRrZuJAEcl7B338qju59zJMjw2DeAQ"
	in, err := svc.normalize(Registration{
		InstallationID: testInstallationID, Platform: "iOS", AppVersion: "1.3.2",
		AppBuild: "142", OSVersion: "26.0", DeviceModel: "iPhone17,1", Locale: "kk-KZ",
		Timezone: "Asia/Almaty", Permission: "authorized",
		Push: &PushToken{Token: fcm},
	})
	if err != nil {
		t.Fatalf("valid registration refused: %v", err)
	}
	if in.Platform != "ios" || in.Provider != "fcm" || in.Locale != "kk" ||
		in.TokenHash != push.TokenHash(fcm) || strings.Contains(in.TokenSealed, fcm) ||
		in.OSName != "iOS" || !in.NotificationsEnabled {
		t.Fatalf("normalised = %+v", in)
	}
	if _, err := svc.normalize(Registration{InstallationID: testInstallationID,
		Platform: "android", Push: &PushToken{Provider: "FCM", Token: fcm}}); err != nil {
		t.Fatalf("fcm token refused: %v", err)
	}
	for name, reg := range map[string]Registration{
		"no id":        {Platform: "ios"},
		"short id":     {InstallationID: "abc", Platform: "ios"},
		"id injection": {InstallationID: "0f8fad5b' OR 1=1 --", Platform: "ios"},
		"platform":     {InstallationID: testInstallationID, Platform: "web"},
		"permission":   {InstallationID: testInstallationID, Platform: "ios", Permission: "maybe"},
		"apns provider": {InstallationID: testInstallationID, Platform: "ios",
			Push: &PushToken{Provider: "apns", Token: strings.Repeat("ab", 32)}},
		"token characters": {InstallationID: testInstallationID, Platform: "ios",
			Push: &PushToken{Token: strings.Repeat("z", 30) + "<>"}},
		"fcm too short": {InstallationID: testInstallationID, Platform: "android",
			Push: &PushToken{Token: "short"}},
		"fcm too long": {InstallationID: testInstallationID, Platform: "android",
			Push: &PushToken{Token: strings.Repeat("a", 4097)}},
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
		InstallationID: testInstallationID, Platform: "android",
		DeviceModel: "SM-S928B<script>", Manufacturer: "samsung", AppVersion: strings.Repeat("9", 80),
		Locale: "<ru>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.DeviceModel != "" || in.Manufacturer != "samsung" || len(in.AppVersion) != 32 || in.Locale != "" ||
		in.HasToken || in.Permission != domain.PermissionUnknown {
		t.Fatalf("metadata = %+v", in)
	}
}
