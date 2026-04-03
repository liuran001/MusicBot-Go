package handler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeExtractedAudioPath_FLACContainerToFLAC(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "track.m4a")
	if err := os.WriteFile(srcPath, []byte("container"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	originalProbe := probeExtractedAudioCodec
	originalExtract := extractEmbeddedFLAC
	originalRemux := remuxExtractedAudioM4A
	t.Cleanup(func() {
		probeExtractedAudioCodec = originalProbe
		extractEmbeddedFLAC = originalExtract
		remuxExtractedAudioM4A = originalRemux
	})

	probeExtractedAudioCodec = func(filePath string) (string, error) {
		if filePath != srcPath {
			t.Fatalf("probe path = %q, want %q", filePath, srcPath)
		}
		return "flac", nil
	}
	extractEmbeddedFLAC = func(_ context.Context, gotSrc, gotDst string) error {
		if gotSrc != srcPath {
			t.Fatalf("extract src = %q, want %q", gotSrc, srcPath)
		}
		if filepath.Ext(gotDst) != ".flac" {
			t.Fatalf("extract dst ext = %q, want .flac", filepath.Ext(gotDst))
		}
		if err := os.WriteFile(gotDst, []byte("flac"), 0o644); err != nil {
			return err
		}
		return os.Remove(gotSrc)
	}
	remuxExtractedAudioM4A = func(context.Context, string, string) error {
		return errors.New("should not remux flac")
	}

	gotPath, gotExt := normalizeExtractedAudioPath(srcPath, "m4a")
	wantPath := filepath.Join(tmpDir, "track.flac")
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if gotExt != "flac" {
		t.Fatalf("ext = %q, want flac", gotExt)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected flac output: %v", err)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("expected source removed, stat err = %v", err)
	}
}

func TestNormalizeExtractedAudioPath_AACOrALACContainerToM4A(t *testing.T) {
	tests := []struct {
		name  string
		codec string
		ext   string
	}{
		{name: "aac mp4 to m4a", codec: "aac", ext: ".mp4"},
		{name: "alac mp4 to m4a", codec: "alac", ext: ".mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			srcPath := filepath.Join(tmpDir, "track"+tt.ext)
			if err := os.WriteFile(srcPath, []byte("container"), 0o644); err != nil {
				t.Fatalf("write source file: %v", err)
			}

			originalProbe := probeExtractedAudioCodec
			originalExtract := extractEmbeddedFLAC
			originalRemux := remuxExtractedAudioM4A
			t.Cleanup(func() {
				probeExtractedAudioCodec = originalProbe
				extractEmbeddedFLAC = originalExtract
				remuxExtractedAudioM4A = originalRemux
			})

			probeExtractedAudioCodec = func(filePath string) (string, error) {
				if filePath != srcPath {
					t.Fatalf("probe path = %q, want %q", filePath, srcPath)
				}
				return tt.codec, nil
			}
			extractEmbeddedFLAC = func(context.Context, string, string) error {
				return errors.New("should not extract flac")
			}
			remuxExtractedAudioM4A = func(_ context.Context, gotSrc, gotDst string) error {
				if gotSrc != srcPath {
					t.Fatalf("remux src = %q, want %q", gotSrc, srcPath)
				}
				if filepath.Ext(gotDst) != ".m4a" {
					t.Fatalf("remux dst ext = %q, want .m4a", filepath.Ext(gotDst))
				}
				if err := os.WriteFile(gotDst, []byte("m4a"), 0o644); err != nil {
					return err
				}
				return os.Remove(gotSrc)
			}

			gotPath, gotExt := normalizeExtractedAudioPath(srcPath, "mp4")
			wantPath := filepath.Join(tmpDir, "track.m4a")
			if gotPath != wantPath {
				t.Fatalf("path = %q, want %q", gotPath, wantPath)
			}
			if gotExt != "m4a" {
				t.Fatalf("ext = %q, want m4a", gotExt)
			}
			if _, err := os.Stat(wantPath); err != nil {
				t.Fatalf("expected m4a output: %v", err)
			}
			if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
				t.Fatalf("expected source removed, stat err = %v", err)
			}
		})
	}
}
