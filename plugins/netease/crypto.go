package netease

import "crypto/aes"

const eapiKey = "e82ckenh8dichen8"
const cacheKey = ")(13daqP@ssw0rd~"
const markerKey = "#14ljk_!\\]&0U<'("

func generateKey(key []byte) []byte {
	genKey := make([]byte, 16)
	copy(genKey, key)
	for i := 16; i < len(key); {
		for j := 0; j < 16 && i < len(key); j, i = j+1, i+1 {
			genKey[j] ^= key[i]
		}
	}
	return genKey
}

func encryptECB(data, keyStr string) []byte {
	origData := []byte(data)
	key := []byte(keyStr)
	cipher, _ := aes.NewCipher(generateKey(key))
	length := (len(origData) + aes.BlockSize) / aes.BlockSize
	plain := make([]byte, length*aes.BlockSize)
	copy(plain, origData)
	pad := byte(len(plain) - len(origData))
	for i := len(origData); i < len(plain); i++ {
		plain[i] = pad
	}
	encrypted := make([]byte, len(plain))
	for bs, be := 0, cipher.BlockSize(); bs <= len(origData); bs, be = bs+cipher.BlockSize(), be+cipher.BlockSize() {
		cipher.Encrypt(encrypted[bs:be], plain[bs:be])
	}
	return encrypted
}

func MarkerEncrypt(data string) []byte {
	return encryptECB(data, markerKey)
}

func CacheKeyEncrypt(data string) []byte {
	return encryptECB(data, cacheKey)
}

func eapiEncrypt(data string) []byte {
	return encryptECB(data, eapiKey)
}
