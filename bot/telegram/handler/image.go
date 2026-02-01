package handler

import (
	"bufio"
	"bytes"
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
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	buffer, err := io.ReadAll(bufio.NewReader(file))
	if err != nil {
		_ = file.Close()
		return "", err
	}

	img, err := jpeg.Decode(bytes.NewReader(buffer))
	if err != nil {
		img, err = png.Decode(bytes.NewReader(buffer))
		if err != nil {
			_ = file.Close()
			return "", fmt.Errorf("image decode error %s", filePath)
		}
	}
	if err := file.Close(); err != nil {
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
	defer out.Close()

	if err := jpeg.Encode(out, newImg, nil); err != nil {
		return "", err
	}
	return filePath + ".resize.jpg", nil
}
