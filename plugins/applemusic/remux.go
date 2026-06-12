package applemusic

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// remuxToProgressive rewrites a fragmented MP4 (the form produced by both the
// native Widevine decrypt and the wrapper cbcs pipeline) into a progressive
// MP4 with a complete moov sample table and faststart layout.
//
// Why this matters: Apple Music streams are fragmented MP4 (ftyp + moov[mvex]
// + repeating moof/mdat). Telegram's inline audio player and many desktop
// players (e.g. Windows) cannot show a duration/progress bar or seek in a
// fragmented MP4 because there is no flat sample table — playback appears
// broken even though the audio data is intact. A stream-copy remux
// (no re-encode, lossless, fast) produces a normal moov+mdat file that plays
// and seeks correctly everywhere.
//
// The operation is in-place: on success the file at path is replaced with the
// progressive version. ffmpeg is required (bundled in the Docker image); if it
// is unavailable the original file is left untouched and an error is returned
// so the caller can decide whether to proceed.
func remuxToProgressive(ctx context.Context, path string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	tmp := path + ".remux.m4a"
	// -c copy: no re-encode (lossless, fast). +faststart: move moov to front.
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y", "-loglevel", "error",
		"-i", path,
		"-c", "copy",
		"-movflags", "+faststart",
		"-f", "mp4",
		tmp,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg remux failed: %w: %s", err, stderr.String())
	}

	// Replace the original atomically (same dir → rename is atomic).
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace original with remuxed: %w", err)
	}
	return nil
}
