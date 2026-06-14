package lyric

import "testing"

// TestDecodeQRCEndToEnd feeds a QRC blob produced by the reference PHP encoder
// (its own triple-DES in encrypt mode + zlib) and asserts Go recovers the body.
func TestDecodeQRCEndToEnd(t *testing.T) {
	const hexBlob = "0c8d67dd3e549974b64ed2680459f13881aa15d10db4cc8324b86311d0d741bd6af5d8724f2b7571b2b2bf976be395e454a23ccb367e464b21d3e3d96109c4a83e6466f98a1f73e1b6b981815534bddc730599b49a3facf5850623404b4910630577949cc16839af296af1ba533aba4fa42f0424822326fcb0f1c6258830a73b61011e253435aef2"
	out, err := DecodeQRC(hexBlob)
	if err != nil {
		t.Fatalf("DecodeQRC error: %v", err)
	}
	const want = "[1000,2000]Hello (1000,500)world (1500,500)"
	if out != want {
		t.Fatalf("DecodeQRC = %q, want %q", out, want)
	}
}
