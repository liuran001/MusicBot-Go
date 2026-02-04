package qqmusic

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

var (
	part1Indexes   = []int{23, 14, 6, 36, 16, 40, 7, 19}
	part2Indexes   = []int{16, 1, 32, 12, 19, 27, 8, 5}
	scrambleValues = []byte{
		89, 39, 179, 150, 218, 82, 58, 252, 177, 52,
		186, 123, 120, 64, 242, 133, 143, 161, 121, 179,
	}
)

func tencentSign(payload string, clearPart1 bool) string {
	if payload == "" {
		return ""
	}
	sum := sha1.Sum([]byte(payload))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))

	part1 := ""
	for _, idx := range part1Indexes {
		if idx < 40 && idx < len(hash) {
			part1 += hash[idx : idx+1]
		}
	}
	if clearPart1 {
		part1 = ""
	}

	part2 := ""
	for _, idx := range part2Indexes {
		if idx < len(hash) {
			part2 += hash[idx : idx+1]
		}
	}

	part3 := make([]byte, 0, len(scrambleValues))
	for i := 0; i < len(scrambleValues); i++ {
		pos := i * 2
		if pos+2 > len(hash) {
			break
		}
		value, err := hex.DecodeString(hash[pos : pos+2])
		if err != nil || len(value) == 0 {
			return ""
		}
		part3 = append(part3, scrambleValues[i]^value[0])
	}

	b64Part := base64.StdEncoding.EncodeToString(part3)
	b64Part = strings.NewReplacer("/", "", "\\", "", "+", "", "=", "").Replace(b64Part)

	return "zzc" + strings.ToLower(part1+b64Part+part2)
}
