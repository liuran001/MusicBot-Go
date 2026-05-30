package util

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func TestCalculateMD5(t *testing.T) {
	data := []byte("hello world")
	path := writeTempFile(t, data)

	got, err := CalculateMD5(path)
	if err != nil {
		t.Fatalf("CalculateMD5 error: %v", err)
	}
	if want := md5Hex(data); got != want {
		t.Fatalf("CalculateMD5 = %q, want %q", got, want)
	}
}

func TestCalculateMD5_MissingFile(t *testing.T) {
	if _, err := CalculateMD5(filepath.Join(t.TempDir(), "nope.bin")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestVerifyMD5(t *testing.T) {
	data := []byte("verify me")
	path := writeTempFile(t, data)
	sum := md5Hex(data)

	ok, err := VerifyMD5(path, sum)
	if err != nil {
		t.Fatalf("VerifyMD5 error: %v", err)
	}
	if !ok {
		t.Fatal("VerifyMD5 = false, want true for matching checksum")
	}

	ok, err = VerifyMD5(path, "deadbeef")
	if err != nil {
		t.Fatalf("VerifyMD5 error: %v", err)
	}
	if ok {
		t.Fatal("VerifyMD5 = true, want false for mismatched checksum")
	}
}

func TestVerifyMD5_EmptyExpectedSkips(t *testing.T) {
	// An empty expected checksum should short-circuit to true without
	// touching the filesystem (the path need not exist).
	ok, err := VerifyMD5(filepath.Join(t.TempDir(), "missing.bin"), "")
	if err != nil {
		t.Fatalf("VerifyMD5 error: %v", err)
	}
	if !ok {
		t.Fatal("VerifyMD5 with empty expected = false, want true")
	}
}

func TestVerifyMD5_MissingFile(t *testing.T) {
	if _, err := VerifyMD5(filepath.Join(t.TempDir(), "missing.bin"), "deadbeef"); err == nil {
		t.Fatal("expected error for missing file with non-empty expected")
	}
}

func TestCopyWithProgress_NilProgressCopiesAll(t *testing.T) {
	src := bytes.NewReader([]byte("abcdef"))
	var dst bytes.Buffer

	n, err := CopyWithProgress(&dst, src, 6, nil)
	if err != nil {
		t.Fatalf("CopyWithProgress error: %v", err)
	}
	if n != 6 || dst.String() != "abcdef" {
		t.Fatalf("CopyWithProgress = (%d, %q), want (6, \"abcdef\")", n, dst.String())
	}
}

func TestCopyWithProgress_ReportsProgressAndCopies(t *testing.T) {
	// Use data larger than the 32KiB pool buffer so the copy loop runs
	// multiple iterations, exercising the pooled buffer reuse path.
	const total = 80 * 1024
	data := bytes.Repeat([]byte("x"), total)
	src := bytes.NewReader(data)
	var dst bytes.Buffer

	var calls int
	var lastWritten int64
	n, err := CopyWithProgress(&dst, src, int64(total), func(written, tot int64) {
		calls++
		if tot != int64(total) {
			t.Errorf("progress total = %d, want %d", tot, total)
		}
		if written < lastWritten {
			t.Errorf("progress written went backwards: %d < %d", written, lastWritten)
		}
		lastWritten = written
	})
	if err != nil {
		t.Fatalf("CopyWithProgress error: %v", err)
	}
	if n != int64(total) {
		t.Fatalf("CopyWithProgress copied %d bytes, want %d", n, total)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Fatal("copied data does not match source")
	}
	if calls < 2 {
		t.Fatalf("expected multiple progress callbacks for >buffer data, got %d", calls)
	}
	if lastWritten != int64(total) {
		t.Fatalf("final progress written = %d, want %d", lastWritten, total)
	}
}

type errReader struct {
	data []byte
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

func TestCopyWithProgress_PropagatesReadError(t *testing.T) {
	wantErr := errors.New("boom")
	src := &errReader{data: []byte("partial"), err: wantErr}
	var dst bytes.Buffer

	n, err := CopyWithProgress(&dst, src, 100, func(int64, int64) {})
	if !errors.Is(err, wantErr) {
		t.Fatalf("CopyWithProgress error = %v, want %v", err, wantErr)
	}
	if n != int64(len("partial")) {
		t.Fatalf("CopyWithProgress wrote %d before error, want %d", n, len("partial"))
	}
	if dst.String() != "partial" {
		t.Fatalf("dst = %q, want %q", dst.String(), "partial")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	// Always claim fewer bytes written than requested to trigger ErrShortWrite.
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestCopyWithProgress_ShortWrite(t *testing.T) {
	src := strings.NewReader("hello")
	_, err := CopyWithProgress(shortWriter{}, src, 5, func(int64, int64) {})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("CopyWithProgress error = %v, want io.ErrShortWrite", err)
	}
}
