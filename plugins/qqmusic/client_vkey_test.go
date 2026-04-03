package qqmusic

import "testing"

func TestBuildVKeyFilenames(t *testing.T) {
	got := buildVKeyFilenames("songmid", "mediamid", "M800", "mp3")
	want := []string{"M800mediamid.mp3", "M800songmidsongmid.mp3"}
	if len(got) != len(want) {
		t.Fatalf("expected %d filenames, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected filename[%d]=%q, got %q", i, want[i], got[i])
		}
	}
}

func TestBuildVKeyFilenames_SkipsEmptyCandidates(t *testing.T) {
	got := buildVKeyFilenames("", "", "", "")
	if len(got) != 0 {
		t.Fatalf("expected no filenames for empty inputs, got %#v", got)
	}
}

func TestResolveVKeyURL_FallbackToWifiURL(t *testing.T) {
	got := resolveVKeyURL("", "https://dl.stream.qqmusic.qq.com/test", "")
	if got != "https://dl.stream.qqmusic.qq.com/test" {
		t.Fatalf("expected wifiurl fallback, got %q", got)
	}
}
