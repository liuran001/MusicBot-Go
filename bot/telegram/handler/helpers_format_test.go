package handler

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8Bytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{name: "no truncation when within limit", input: "hello", maxBytes: 10, want: "hello"},
		{name: "exact length kept", input: "hello", maxBytes: 5, want: "hello"},
		{name: "zero budget returns original", input: "hello", maxBytes: 0, want: "hello"},
		{name: "negative budget returns original", input: "hello", maxBytes: -3, want: "hello"},
		{name: "ascii truncated", input: "hello world", maxBytes: 5, want: "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateUTF8Bytes(tt.input, tt.maxBytes); got != tt.want {
				t.Fatalf("truncateUTF8Bytes(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.want)
			}
		})
	}
}

func TestTruncateUTF8BytesNeverSplitsRune(t *testing.T) {
	// Each "测" is 3 bytes. Cutting at a non-multiple-of-3 byte budget must
	// not leave a partial (invalid) rune in the result.
	s := strings.Repeat("测", 10) // 30 bytes
	for budget := 1; budget <= 30; budget++ {
		got := truncateUTF8Bytes(s, budget)
		if !utf8.ValidString(got) {
			t.Fatalf("budget=%d produced invalid UTF-8: %q", budget, got)
		}
		if len(got) > budget {
			t.Fatalf("budget=%d produced %d bytes, exceeds budget", budget, len(got))
		}
	}
}

func TestFormatFileInfo(t *testing.T) {
	tests := []struct {
		name      string
		fileExt   string
		musicSize int
		want      string
	}{
		{name: "empty ext yields empty", fileExt: "", musicSize: 1024 * 1024, want: ""},
		{name: "whitespace ext yields empty", fileExt: "   ", musicSize: 1024 * 1024, want: ""},
		{name: "zero size yields empty", fileExt: "mp3", musicSize: 0, want: ""},
		{name: "negative size yields empty", fileExt: "mp3", musicSize: -5, want: ""},
		{name: "one megabyte", fileExt: "mp3", musicSize: 1024 * 1024, want: "mp3 1.00MB"},
		{name: "half megabyte", fileExt: "flac", musicSize: 512 * 1024, want: "flac 0.50MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFileInfo(tt.fileExt, tt.musicSize); got != tt.want {
				t.Fatalf("formatFileInfo(%q, %d) = %q, want %q", tt.fileExt, tt.musicSize, got, tt.want)
			}
		})
	}
}
