package apptest

import (
	"strings"
	"testing"
)

func TestLandingAdvertisesOnlyPublishedMacRelease(t *testing.T) {
	for _, release := range []string{"", "https://example.test/downloads/macos/AI-Reply-1.0.0.dmg"} {
		h := newHarness(t, withEnv("PUBLIC_BASE_URL", "https://example.test"), withEnv("MACOS_DOWNLOAD_URL", release))
		_, body := fetch(t, h, "/?lang=en")
		if !strings.Contains(body, `href="/mac?lang=en"`) {
			t.Fatal("landing has no Mac information link")
		}
		if strings.Contains(body, ">Available now</small><b>macOS</b>") != (release != "") {
			t.Fatal("landing advertises a Mac download that has not been published")
		}
	}
}
