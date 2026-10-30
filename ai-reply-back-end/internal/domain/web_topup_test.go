package domain

import "testing"

func TestExternalWebTopupClientCompatibility(t *testing.T) {
	for _, tc := range []struct {
		platform, version, build string
		want                     bool
	}{
		{PlatformIOS, "1.2.0", "14", true},
		{PlatformIOS, "1.2.0", "13", false},
		{PlatformIOS, "1.1.99", "14", false},
		{PlatformIOS, "unknown", "14", false},
		{PlatformIOS, "1.2.0", "14beta", false},
		{PlatformIOS, "1.2.0", "+14", false},
		{PlatformIOS, "1.2.0", "", false},
		{PlatformAndroid, "1.2.0", "14", false},
	} {
		got := (Installation{Platform: tc.platform, AppVersion: tc.version, AppBuild: tc.build}).SupportsExternalWebTopup()
		if got != tc.want {
			t.Errorf("%s %s (%s): %v", tc.platform, tc.version, tc.build, got)
		}
	}
}
