package handler

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/nfnt/resize"
)

// resizeImg scales the image to 320x320 with padding.
func resizeImg(filePath string) (string, error) {
	img, err := decodeJPEGOrPNG(filePath)
	if err != nil {
		return "", err
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	widthNew := 320
	heightNew := 320

	var m image.Image
	if width/height >= widthNew/heightNew {
		m = resize.Resize(uint(widthNew), uint(height)*uint(widthNew)/uint(width), img, resize.Lanczos3)
	} else {
		m = resize.Resize(uint(width*heightNew/height), uint(heightNew), img, resize.Lanczos3)
	}

	newImg := image.NewNRGBA(image.Rect(0, 0, 320, 320))
	if m.Bounds().Dx() > m.Bounds().Dy() {
		draw.Draw(newImg, image.Rectangle{
			Min: image.Point{Y: (320 - m.Bounds().Dy()) / 2},
			Max: image.Point{X: 320, Y: 320},
		}, m, m.Bounds().Min, draw.Src)
	} else {
		draw.Draw(newImg, image.Rectangle{
			Min: image.Point{X: (320 - m.Bounds().Dx()) / 2},
			Max: image.Point{X: 320, Y: 320},
		}, m, m.Bounds().Min, draw.Src)
	}

	out, err := os.Create(filePath + ".resize.jpg")
	if err != nil {
		return "", fmt.Errorf("create image file error %s", err)
	}

	if err := jpeg.Encode(out, newImg, &jpeg.Options{Quality: 85}); err != nil {
		_ = out.Close()
		return "", err
	}
	if stat, err := out.Stat(); err == nil && stat.Size() > 200*1024 {
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			_ = out.Close()
			return "", err
		}
		if err := out.Truncate(0); err != nil {
			_ = out.Close()
			return "", err
		}
		if err := jpeg.Encode(out, newImg, &jpeg.Options{Quality: 60}); err != nil {
			_ = out.Close()
			return "", err
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return filePath + ".resize.jpg", nil
}

func decodeJPEGOrPNG(filePath string) (image.Image, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	img, jpegErr := jpeg.Decode(file)
	if jpegErr == nil {
		return img, nil
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	img, pngErr := png.Decode(file)
	if pngErr == nil {
		return img, nil
	}

	return nil, fmt.Errorf("image decode error %s", filePath)
}
