package handler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsValidFLACFile(t *testing.T) {
	tmpDir := t.TempDir()

	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{name: "valid fLaC magic", content: []byte{0x66, 0x4C, 0x61, 0x43, 0x00, 0x00}, want: true},
		{name: "exactly 4 magic bytes", content: []byte{0x66, 0x4C, 0x61, 0x43}, want: true},
		{name: "wrong magic", content: []byte{0x49, 0x44, 0x33, 0x04}, want: false},
		// Short read: file smaller than the 4-byte header must not pass via
		// stale zero bytes. io.ReadFull returns an error here, file.Read would not.
		{name: "short file 3 bytes", content: []byte{0x66, 0x4C, 0x61}, want: false},
		{name: "empty file", content: []byte{}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(tmpDir, tc.name)
			if err := os.WriteFile(p, tc.content, 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if got := isValidFLACFile(p); got != tc.want {
				t.Errorf("isValidFLACFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsValidFLACFile_MissingFile(t *testing.T) {
	if isValidFLACFile(filepath.Join(t.TempDir(), "does-not-exist.flac")) {
		t.Error("expected false for missing file")
	}
}
