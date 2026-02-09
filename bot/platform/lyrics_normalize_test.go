package platform

import "testing"

func TestNormalizeLRCTimestamps(t *testing.T) {
	input := "[00:01:10]A\n[00:06.97]B\n[01:02.3]C\n[01:02.345]D\n[invalid]E"
	want := "[00:01.10]A\n[00:06.97]B\n[01:02.30]C\n[01:02.34]D\n[invalid]E"

	got := NormalizeLRCTimestamps(input)
	if got != want {
		t.Fatalf("NormalizeLRCTimestamps() = %q, want %q", got, want)
	}
}
