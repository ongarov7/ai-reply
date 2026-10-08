package auth

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

// Белгісіз пошта үшін тексеру нақты хэшпен бірдей бағалы: сол итерация, сол ұзындықтар.
func TestDummyHashCostsAsMuchAsARealOne(t *testing.T) {
	real, err := HashPassword("an-admin-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	dummy, actual := strings.Split(dummyHash, "$"), strings.Split(real, "$")
	if len(dummy) != 4 || dummy[0] != actual[0] || dummy[1] != strconv.Itoa(pbkdf2Iterations) || dummy[1] != actual[1] {
		t.Fatalf("dummy hash %q does not match the real layout %q", dummyHash, real)
	}
	for i, size := range map[int]int{2: pbkdf2SaltLength, 3: pbkdf2KeyLength} {
		raw, err := base64.RawStdEncoding.DecodeString(dummy[i])
		if err != nil || len(raw) != size {
			t.Fatalf("part %d: %d bytes, %v", i, len(raw), err)
		}
	}
	// It never matches, whatever is typed.
	for _, password := range []string{"", "an-admin-passphrase", strings.Repeat("\x00", 32)} {
		if VerifyPassword(dummyHash, password) == nil {
			t.Fatalf("the dummy hash accepted %q", password)
		}
	}
	VerifyDummyPassword("anything")
}
