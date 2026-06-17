package netease

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRecognizeFingerprint exercises the full pure-Go fingerprint pipeline:
// ffmpeg decode -> afp.wasm ExtractQueryFP (via wazero+embind) -> base64.
// It is skipped when ffmpeg or the wasm module are unavailable so it stays
// green in minimal CI environments.
func TestRecognizeFingerprint(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := loadAFPWasm(); err != nil {
		t.Skipf("afp.wasm not available: %v", err)
	}

	audio, err := os.ReadFile(filepath.Join("testdata", "recognize_tone.mp3"))
	if err != nil {
		t.Skipf("testdata audio missing: %v", err)
	}

	svc := NewRecognizeService(0)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := svc.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	ctx := context.Background()

	pcm, err := decodePCM(ctx, audio)
	if err != nil {
		t.Fatalf("decodePCM: %v", err)
	}
	if len(pcm) < afpMinSamples {
		t.Fatalf("decoded PCM too short: got %d samples, need >= %d", len(pcm), afpMinSamples)
	}

	first, err := svc.encodeFingerprint(ctx, pcm)
	if err != nil {
		t.Fatalf("encodeFingerprint: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("empty fingerprint")
	}

	// Golden value: byte-identical to the upstream ncm-audio-recognize JS
	// reference (Node.js encode()) run on the same audio. Guards against
	// regressions in the ffmpeg decode params or the WASM fingerprint path.
	const golden = "uLsYcabnjbmgFsxqyv/mkSfnvs84rKhuzILZ1sIWCKFTmQrAiALs19gmp0mTLoUfTcwwQmCt3e91anpdOf7iou5mRUD//+7NrXebqT/4I47s0AfFN2tAoKqNQC9UwFGzNe27ltsCVc56fDNkT45g6M/Fv5F/KSCbMvCInoxwcq+DFgKZ47VPrX0ndfdY+eBqeCjhX2L8sYfkA0VT8Rgiyg=="
	if first != golden {
		t.Fatalf("fingerprint mismatch:\n  got=%s\n want=%s", first, golden)
	}

	// The fingerprint must be deterministic across repeated calls on the same
	// engine instance (verifies WASM state is properly reset per call).
	second, err := svc.encodeFingerprint(ctx, pcm)
	if err != nil {
		t.Fatalf("encodeFingerprint (2nd): %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint not deterministic:\n  1st=%s\n  2nd=%s", first, second)
	}

	t.Logf("fingerprint (%d base64 chars): %s", len(first), first)
}

// TestRecognizeAudioTooShort verifies the short-audio guard rejects input that
// cannot fill the fingerprint window without invoking the WASM module.
func TestRecognizeAudioTooShort(t *testing.T) {
	svc := NewRecognizeService(0)
	// Not started; a too-short PCM slice must fail before reaching the engine.
	_, err := svc.encodeFingerprint(context.Background(), make([]float32, 1000))
	if err == nil {
		t.Fatal("expected error for unstarted service / short input")
	}
}
