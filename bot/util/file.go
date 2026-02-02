package util

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
)

func VerifyMD5(filePath, expected string) (bool, error) {
	if expected == "" {
		return true, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}

	calculated := hex.EncodeToString(hash.Sum(nil))
	return calculated == expected, nil
}

func CalculateMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

type ProgressFunc func(written, total int64)

func CopyWithProgress(dst io.Writer, src io.Reader, total int64, progress ProgressFunc) (int64, error) {
	if progress == nil {
		return io.Copy(dst, src)
	}

	buf := make([]byte, 32*1024)
	var written int64

	for {
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				progress(written, total)
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if err != nil {
			if err == io.EOF {
				return written, nil
			}
			return written, err
		}
	}
}
