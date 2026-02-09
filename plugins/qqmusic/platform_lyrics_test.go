package qqmusic

import "testing"

func TestParseLyricLines_AutoFixMalformedTimestamp(t *testing.T) {
	lrc := `[00:00.00]Line 1
[00:01:10]Line 2
[00:06:97]Line 3
[invalid]skip`

	lines := parseLyricLines(lrc)
	if len(lines) != 3 {
		t.Fatalf("expected 3 parsed lines, got %d", len(lines))
	}

	if lines[1].Time.Milliseconds() != 1100 {
		t.Fatalf("expected second line time 1100ms, got %dms", lines[1].Time.Milliseconds())
	}

	if lines[2].Time.Milliseconds() != 6970 {
		t.Fatalf("expected third line time 6970ms, got %dms", lines[2].Time.Milliseconds())
	}
}
