package happ

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"sync"
)

var (
	legacyKeysMu sync.Mutex
	legacyKeys   = map[int]*rsa.PrivateKey{}
)

// legacyKey parses and caches one of the embedded PKCS#1 keys.
func legacyKey(ordinal int) (*rsa.PrivateKey, error) {
	if ordinal < 0 || ordinal >= len(legacyRSAKeys) {
		return nil, fmt.Errorf("happ: legacy key index %d is out of range", ordinal)
	}

	legacyKeysMu.Lock()
	defer legacyKeysMu.Unlock()

	if key, ok := legacyKeys[ordinal]; ok {
		return key, nil
	}

	der, err := base64.StdEncoding.DecodeString(legacyRSAKeys[ordinal])
	if err != nil {
		return nil, fmt.Errorf("happ: embedded key %d is not valid base64: %w", ordinal, err)
	}
	key, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("happ: embedded key %d is not a valid RSA key: %w", ordinal, err)
	}
	legacyKeys[ordinal] = key
	return key, nil
}

// decryptLegacy handles the crypt/crypt2/crypt3/crypt4 generations: the payload
// is one or more RSA PKCS#1 v1.5 blocks that concatenate into the plaintext URL.
func decryptLegacy(ordinal int, payload string) (string, error) {
	key, err := legacyKey(ordinal)
	if err != nil {
		return "", err
	}

	ciphertext, err := base64Decode(payload)
	if err != nil {
		return "", fmt.Errorf("happ: %s payload is not valid base64: %w", generationName(ordinal), err)
	}

	blockSize := key.Size()
	if len(ciphertext) == 0 || len(ciphertext)%blockSize != 0 {
		return "", fmt.Errorf(
			"happ: %s payload is %d bytes, expected a multiple of %d",
			generationName(ordinal), len(ciphertext), blockSize,
		)
	}

	plaintext := make([]byte, 0, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += blockSize {
		//nolint:staticcheck // happ 加密格式固定使用 PKCS#1 v1.5，换成 OAEP 将无法解密既有链接。
		block, err := rsa.DecryptPKCS1v15(nil, key, ciphertext[offset:offset+blockSize])
		if err != nil {
			return "", fmt.Errorf("happ: %s decryption failed: %w", generationName(ordinal), err)
		}
		plaintext = append(plaintext, block...)
	}
	return string(plaintext), nil
}

func generationName(ordinal int) string {
	switch ordinal {
	case 0:
		return "crypt"
	case 1:
		return "crypt2"
	case 2:
		return "crypt3"
	case 3:
		return "crypt4"
	default:
		return "crypt"
	}
}
