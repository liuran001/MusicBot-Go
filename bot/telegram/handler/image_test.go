package handler

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestResizeImgJPEG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jpg")
	writeSolidImageAsJPEG(t, inputPath, 640, 360)

	resizedPath, err := resizeImg(inputPath)
	if err != nil {
		t.Fatalf("resizeImg jpeg failed: %v", err)
	}

	if resizedPath != inputPath+".resize.jpg" {
		t.Fatalf("unexpected resized path: %s", resizedPath)
	}

	decoded := mustDecodeJPEG(t, resizedPath)
	if gotW, gotH := decoded.Bounds().Dx(), decoded.Bounds().Dy(); gotW != 320 || gotH != 320 {
		t.Fatalf("expected 320x320, got %dx%d", gotW, gotH)
	}

	if stat, err := os.Stat(resizedPath); err != nil {
		t.Fatalf("expected resized file exists: %v", err)
	} else if stat.Size() <= 0 {
		t.Fatalf("expected resized file has content")
	}
}

func TestResizeImgPNG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.png")
	writeSolidImageAsPNG(t, inputPath, 480, 960)

	resizedPath, err := resizeImg(inputPath)
	if err != nil {
		t.Fatalf("resizeImg png failed: %v", err)
	}

	decoded := mustDecodeJPEG(t, resizedPath)
	if gotW, gotH := decoded.Bounds().Dx(), decoded.Bounds().Dy(); gotW != 320 || gotH != 320 {
		t.Fatalf("expected 320x320, got %dx%d", gotW, gotH)
	}
}

func TestResizeImgDecodeError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "invalid.img")
	if err := os.WriteFile(inputPath, []byte("not-an-image"), 0o644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}

	if _, err := resizeImg(inputPath); err == nil {
		t.Fatalf("expected decode error for invalid image")
	}
}

func writeSolidImageAsJPEG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fillSolid(img, color.RGBA{R: 200, G: 100, B: 50, A: 255})

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
}

func writeSolidImageAsPNG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	fillSolid(img, color.RGBA{R: 50, G: 120, B: 220, A: 255})

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

func mustDecodeJPEG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jpeg: %v", err)
	}
	defer func() { _ = f.Close() }()

	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}
	return img
}

func fillSolid(img *image.RGBA, c color.RGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}
