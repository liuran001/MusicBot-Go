package native

import "testing"

// TestDeriveSecret pins the TOTP secret derivation against the known v61 vector.
func TestDeriveSecret(t *testing.T) {
	got := string(deriveSecret(builtinTOTPCipher))
	want := "376136387538459893883312310911992847112448894410210511297108"
	if got != want {
		t.Fatalf("deriveSecret(v61) = %q, want %q", got, want)
	}
}

// TestGenerateTOTP pins the full TOTP computation against vectors verified
// against the official web-player derivation (xyloflake/spot-secrets-go +
// votify). Counter = serverTimeMs/1000/30.
func TestGenerateTOTP(t *testing.T) {
	secret := deriveSecret(builtinTOTPCipher)
	cases := []struct {
		ms   int64
		want string
	}{
		{1700000000000, "371599"},
		{1735689600000, "067144"},
		{1751000000000, "170266"},
	}
	for _, c := range cases {
		if got := generateTOTP(secret, c.ms); got != c.want {
			t.Errorf("generateTOTP(%d) = %s, want %s", c.ms, got, c.want)
		}
	}
}
