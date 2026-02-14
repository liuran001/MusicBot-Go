package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyToPath_NoProgress_CopiesContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	data := []byte("hello-copy-to-path")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	written, err := copyToPath(src, dst, int64(len(data)), nil)
	if err != nil {
		t.Fatalf("copyToPath failed: %v", err)
	}
	if written != int64(len(data)) {
		t.Fatalf("unexpected written bytes: got %d want %d", written, len(data))
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content mismatch: got %q want %q", string(got), string(data))
	}
}
