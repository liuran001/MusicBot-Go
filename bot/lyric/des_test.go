package lyric

import (
	"encoding/hex"
	"testing"
)

// TestQRCDESMatchesPHP pins the ported bespoke triple-DES to the exact output of
// the reference PHP QrcDecode\Decoder for a known 16-byte ciphertext. The PHP
// value was produced via its private tripledes_crypt pipeline (decrypt mode).
func TestQRCDESMatchesPHP(t *testing.T) {
	cipher, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	got := hex.EncodeToString(qrcDESDecrypt(cipher))
	const wantPHP = "3f706d43d53196a860bb351710e55957"
	if got != wantPHP {
		t.Fatalf("qrcDESDecrypt = %s, want PHP %s", got, wantPHP)
	}
}
