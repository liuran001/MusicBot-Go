package kugou

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strings"
)

const conceptLitePublicKeyPEM = "-----BEGIN PUBLIC KEY-----\nMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDECi0Np2UR87scwrvTr72L6oO01rBbbBPriSDFPxr3Z5syug0O24QyQO8bg27+0+4kBzTBTBOZ/WWU0WryL1JSXRTXLgFVxtzIY41Pe7lPOgsfTCn5kZcvKhYKJesKnnJDNr5/abvTGf+rHG3YRwsCHcQ08/q6ifSioBszvb3QiwIDAQAB\n-----END PUBLIC KEY-----"

func conceptMD5(text string) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func conceptSignatureWeb(params url.Values) string {
	keys := sortedQueryKeys(params)
	var builder strings.Builder
	for _, key := range keys {
		for _, value := range params[key] {
			builder.WriteString(key)
			builder.WriteByte('=')
			builder.WriteString(value)
		}
	}
	return conceptMD5(kugouPlaySignKey + builder.String() + kugouPlaySignKey)
}

func conceptSignatureAndroid(params url.Values, body string) string {
	keys := sortedQueryKeys(params)
	var builder strings.Builder
	for _, key := range keys {
		for _, value := range params[key] {
			builder.WriteString(key)
			builder.WriteByte('=')
			builder.WriteString(value)
		}
	}
	return conceptMD5(kugouConceptSignSecret + builder.String() + body + kugouConceptSignSecret)
}

func conceptSignKey(hash, mid, userID, appID string) string {
	return conceptMD5(strings.ToLower(strings.TrimSpace(hash)) + kugouConceptPlaySecret + strings.TrimSpace(appID) + strings.TrimSpace(mid) + strings.TrimSpace(userID))
}

func conceptAESCBCEncryptHex(payload any, key, iv string) string {
	plain := normalizeConceptPayload(payload)
	return conceptAESCBCEncryptHexBytes(plain, []byte(key), []byte(iv))
}

func conceptAESCBCEncryptStringHex(text, key, iv string) string {
	return conceptAESCBCEncryptHexBytes([]byte(text), []byte(key), []byte(iv))
}

func conceptAESCBCEncryptHexAuto(payload any) (string, string, error) {
	tempKey := strings.ToLower(conceptRandomAlphaNum(16))
	key := conceptMD5(tempKey)[:32]
	iv := key[len(key)-16:]
	return conceptAESCBCEncryptHex(payload, key, iv), tempKey, nil
}

func conceptAESCBCDecryptHex(cipherHex, key, iv string) (string, error) {
	data, err := hex.DecodeString(strings.TrimSpace(cipherHex))
	if err != nil {
		return "", err
	}
	plain, err := conceptAESCBCDecryptBytes(data, []byte(key), []byte(iv))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func conceptPlaylistAesEncrypt(payload any) (string, string, error) {
	tempKey := strings.ToLower(conceptRandomAlpha(6))
	md5Text := conceptMD5(tempKey)
	key := md5Text[:16]
	iv := md5Text[16:32]
	plain := normalizeConceptPayload(payload)
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", "", err
	}
	plain = pkcs7Pad(plain, block.BlockSize())
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte(iv)).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted), tempKey, nil
}

func conceptPlaylistAesDecryptFromBinary(body []byte, tempKey string) (string, error) {
	md5Text := conceptMD5(tempKey)
	key := md5Text[:16]
	iv := md5Text[16:32]
	decoded, err := base64.StdEncoding.DecodeString(base64.StdEncoding.EncodeToString(body))
	if err != nil {
		return "", err
	}
	plain, err := conceptAESCBCDecryptBytes(decoded, []byte(key), []byte(iv))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func conceptRSAPKCS1v15EncryptHex(payload any, publicKeyPEM string) (string, error) {
	pub, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}
	plain := normalizeConceptPayload(payload)
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pub, plain)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(encrypted), nil
}

func conceptRSARawEncryptHex(payload any, publicKeyPEM string) (string, error) {
	pub, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}
	plain := normalizeConceptPayload(payload)
	keyLength := (pub.N.BitLen() + 7) / 8
	if len(plain) > keyLength {
		return "", fmt.Errorf("rsa payload exceeds key size")
	}
	padded := make([]byte, keyLength)
	copy(padded, plain)
	m := new(big.Int).SetBytes(padded)
	e := big.NewInt(int64(pub.E))
	c := new(big.Int).Exp(m, e, pub.N)
	return fmt.Sprintf("%0*x", keyLength*2, c), nil
}

func conceptCalculateMid(guid string) string {
	digest := conceptMD5(guid)
	n := new(big.Int)
	n.SetString(digest, 16)
	return n.String()
}

func conceptRandomGUID() string {
	parts := []int{8, 4, 4, 4, 12}
	out := make([]string, 0, len(parts))
	for _, size := range parts {
		buf := make([]byte, size/2)
		if _, err := rand.Read(buf); err != nil {
			return strings.ToLower(conceptRandomAlphaNum(32))
		}
		out = append(out, hex.EncodeToString(buf))
	}
	return strings.Join(out, "-")
}

func conceptRandomAlphaNum(length int) string {
	const alphabet = "1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return conceptRandomFromCharset(length, alphabet)
}

func conceptRandomAlpha(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	return conceptRandomFromCharset(length, alphabet)
}

func conceptRandomFromCharset(length int, charset string) string {
	if length <= 0 {
		return ""
	}
	buf := make([]byte, length)
	randBytes := make([]byte, length)
	if _, err := rand.Read(randBytes); err != nil {
		return strings.Repeat(string(charset[0]), length)
	}
	for i := range buf {
		buf[i] = charset[int(randBytes[i])%len(charset)]
	}
	return string(buf)
}

func normalizeConceptPayload(payload any) []byte {
	switch v := payload.(type) {
	case nil:
		return []byte("")
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		encoded, _ := json.Marshal(v)
		return encoded
	}
}

func conceptAESCBCEncryptHexBytes(plain, key, iv []byte) string {
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	plain = pkcs7Pad(plain, block.BlockSize())
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)
	return hex.EncodeToString(encrypted)
}

func conceptAESCBCDecryptBytes(ciphertext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid ciphertext length")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	return pkcs7Unpad(plain, block.BlockSize())
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	if padding == 0 {
		padding = blockSize
	}
	pad := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, pad...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 data")
	}
	padding := int(data[len(data)-1])
	if padding <= 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func parseRSAPublicKey(publicKeyPEM string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid pem")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not rsa public key")
	}
	return pub, nil
}

func sortedQueryKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
