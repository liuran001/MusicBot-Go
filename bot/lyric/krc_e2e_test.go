package lyric

import (
	"strings"
	"testing"
)

// TestDecodeKRCEndToEnd feeds a KRC blob (krc1 magic + XOR + zlib) and asserts
// Go recovers the text and parses relative word offsets to absolute timing.
func TestDecodeKRCEndToEnd(t *testing.T) {
	const b64 = "a3JjMTjb6rkSg14OfBi4yET8p91Kia48WYBmzP0qumf8E8zvRkFnTRhyVpGXdvY5BuTemgiKqL4JgnU2cTTU1IvrRzWRgUHS2JRk4rdWqD1uA2RfYPFk78Gh7dBbNpNM0X0="
	text, err := DecodeKRC(b64)
	if err != nil {
		t.Fatalf("DecodeKRC error: %v", err)
	}
	if !strings.Contains(text, "[1000,2000]<0,500,0>Hello") {
		t.Fatalf("decoded KRC unexpected: %q", text)
	}
	res := ParseKRC(text)
	// Word offsets are relative to line start: 1000+0=1000, 1000+500=1500.
	if !strings.Contains(res.RawQRC, "Hello (1000,500)") {
		t.Fatalf("KRC RawQRC missing absolute word timing: %q", res.RawQRC)
	}
	if !strings.Contains(res.RawQRC, "world(1500,500)") {
		t.Fatalf("KRC RawQRC second word wrong: %q", res.RawQRC)
	}
	// And it should convert cleanly to LRC.
	lrc := Convert(Payload{RawQRC: res.RawQRC}, "lrc", Options{})
	if !strings.Contains(lrc, "[00:01.00]Hello world") {
		t.Fatalf("KRC->LRC wrong: %q", lrc)
	}
}
