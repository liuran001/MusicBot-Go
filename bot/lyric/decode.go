package lyric

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strings"
)

// qrcDESKey is the fixed 3DES key QQ Music uses for QRC lyric encryption.
//
// The reference PHP implementation (luren-dc/QQMusicApi) hand-rolls a triple-DES
// whose key schedule — decrypt(key[16:24]) ∘ encrypt(key[8:16]) ∘ decrypt(key[0:8])
// — is exactly standard 3DES-EDE decryption, so Go's crypto/des reproduces it.
var qrcDESKey = []byte("!@#)(*$%123ZXC!@!@#)(NHL")

var qrcLyricContentRe = regexp.MustCompile(`(?s)<Lyric_1\s+[^>]*LyricContent="([^"]*)"`)

// DecodeQRC decrypts a QQ Music QRC payload and returns the inner LyricContent
// token text (the "[start,dur]word(start,dur)…" body), or an error.
//
// input may be either the hex-encoded encrypted blob (from musicu.fcg's
// data.lyric when qrc=1) or already-decrypted XML beginning with "<".
func DecodeQRC(input string) (string, error) {
	xmlContent, err := DecodeQRCXML(input)
	if err != nil {
		return "", err
	}
	content := ExtractQRCLyricContent(xmlContent)
	if content == "" {
		return "", errors.New("qrc: LyricContent not found")
	}
	return content, nil
}

// DecodeQRCXML decrypts a QRC payload to its raw XML form. input may be a
// hex-encoded encrypted blob or already-decrypted XML/token text.
func DecodeQRCXML(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("qrc: empty input")
	}
	if strings.HasPrefix(input, "<") {
		return input, nil
	}
	// Bare token text (already decrypted, no XML wrapper).
	if lineHeadRe.MatchString(input) {
		return input, nil
	}
	return decodeQRCBlob(input)
}

// ExtractQRCLyricContent pulls the primary Lyric_1 LyricContent token body from
// a decrypted QRC XML, falling back to bare token text. Returns "" if absent.
func ExtractQRCLyricContent(xmlContent string) string {
	m := qrcLyricContentRe.FindStringSubmatch(xmlContent)
	if m == nil {
		if lineHeadRe.MatchString(strings.TrimSpace(xmlContent)) {
			return strings.TrimSpace(xmlContent)
		}
		return ""
	}
	return htmlUnescape(m[1])
}

// ExtractQRCExtraContent pulls the romanization track from the Lyric_2 / Lyric_3
// nodes of a decrypted QRC XML. Returns "" if absent.
func ExtractQRCExtraContent(xmlContent string) string {
	return DecodeQRCExtra(xmlContent)
}

// decodeQRCBlob hex-decodes, 3DES-ECB-decrypts and zlib-inflates a QRC blob.
func decodeQRCBlob(hexStr string) (string, error) {
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", err
	}
	if len(raw) < 8 {
		return "", errors.New("qrc: blob too short")
	}
	decrypted := qrcDESDecrypt(raw)
	return inflateZlib(decrypted)
}

var qrcExtraContentRes = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<Lyric_2\s+[^>]*LyricContent="([^"]*)"`),
	regexp.MustCompile(`(?s)<Lyric_3\s+[^>]*LyricContent="([^"]*)"`),
}

// DecodeQRCExtra extracts the romanization track from a decrypted QRC XML, which
// some songs store in Lyric_2 / Lyric_3 nodes. Returns "" if absent.
func DecodeQRCExtra(xmlContent string) string {
	for _, re := range qrcExtraContentRes {
		if m := re.FindStringSubmatch(xmlContent); m != nil {
			return htmlUnescape(m[1])
		}
	}
	return ""
}

// kugouKRCKey is the fixed 16-byte XOR key for Kugou KRC lyric decryption.
var kugouKRCKey = []byte{0x40, 0x47, 0x61, 0x77, 0x5e, 0x32, 0x74, 0x47, 0x51, 0x36, 0x31, 0x2d, 0xce, 0xd2, 0x6e, 0x69}

// DecodeKRC decrypts a Kugou KRC payload. content is the base64 string from the
// lyrics.kugou.com download API's "content" field. The result is the decrypted
// KRC text ("[startMs,durMs]<relStart,dur,0>word…" lines plus header tags).
func DecodeKRC(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("krc: empty content")
	}
	blob, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return "", err
	}
	if len(blob) < 4 {
		return "", errors.New("krc: content too short")
	}
	enc := blob[4:] // skip 4-byte "krc1" magic
	plain := make([]byte, len(enc))
	for i, b := range enc {
		plain[i] = b ^ kugouKRCKey[i%len(kugouKRCKey)]
	}
	return inflateZlib(plain)
}

func inflateZlib(data []byte) (string, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

var htmlEntityReplacer = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", "\"",
	"&#39;", "'",
	"&apos;", "'",
)

func htmlUnescape(s string) string {
	return htmlEntityReplacer.Replace(s)
}
