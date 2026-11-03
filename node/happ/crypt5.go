package happ

import (
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// crypt5KeysJSON holds the PKCS#8 private keys used by the crypt5 generation,
// keyed by the eight character marker embedded in every link.
//
//go:embed crypt5keys.json
var crypt5KeysJSON []byte

var (
	crypt5KeysOnce sync.Once
	crypt5Keys     map[string]*rsa.PrivateKey
	crypt5KeysErr  error
)

// crypt5KeyTable parses and caches the embedded key table once per process.
func crypt5KeyTable() (map[string]*rsa.PrivateKey, error) {
	crypt5KeysOnce.Do(func() {
		var raw map[string]string
		if err := json.Unmarshal(crypt5KeysJSON, &raw); err != nil {
			crypt5KeysErr = fmt.Errorf("happ: embedded crypt5 key table is not valid JSON: %w", err)
			return
		}

		parsed := make(map[string]*rsa.PrivateKey, len(raw))
		for marker, encoded := range raw {
			der, err := base64Decode(encoded)
			if err != nil {
				crypt5KeysErr = fmt.Errorf("happ: crypt5 key %q is not valid base64: %w", marker, err)
				return
			}
			value, err := x509.ParsePKCS8PrivateKey(der)
			if err != nil {
				crypt5KeysErr = fmt.Errorf("happ: crypt5 key %q is not a valid PKCS#8 key: %w", marker, err)
				return
			}
			key, ok := value.(*rsa.PrivateKey)
			if !ok {
				crypt5KeysErr = fmt.Errorf("happ: crypt5 key %q is not an RSA key", marker)
				return
			}
			parsed[marker] = key
		}
		crypt5Keys = parsed
	})
	return crypt5Keys, crypt5KeysErr
}

// decryptCrypt5 handles the current generation: the payload is shuffled in
// four byte blocks, the outer eight characters select a private key, and the
// remaining body carries an RSA wrapped ChaCha20-Poly1305 key.
func decryptCrypt5(payload string) (string, error) {
	shuffled := swapBlockHalves([]byte(payload))
	if len(shuffled) < 9 {
		return "", errors.New("happ: crypt5 payload is too short")
	}

	marker := string(shuffled[:4]) + string(shuffled[len(shuffled)-4:])
	keys, err := crypt5KeyTable()
	if err != nil {
		return "", err
	}
	key, ok := keys[marker]
	if !ok {
		return "", fmt.Errorf("%w: crypt5 marker %q is unknown", ErrUnsupported, marker)
	}

	body := shuffled[4 : len(shuffled)-4]
	if len(body) < 13 {
		return "", errors.New("happ: crypt5 payload is too short")
	}

	// Newer links prefix the body with a salt; a decimal segment length at
	// offset 12 marks the legacy layout.
	preferSalted := len(body) > 12 && !isASCIIDigit(body[12])
	plaintext, firstErr := decryptCrypt5Body(body, key, preferSalted)
	if firstErr == nil {
		return plaintext, nil
	}
	if plaintext, err := decryptCrypt5Body(body, key, !preferSalted); err == nil {
		return plaintext, nil
	}
	return "", firstErr
}

func decryptCrypt5Body(body []byte, key *rsa.PrivateKey, salted bool) (string, error) {
	nonce := body[:12]
	var salt []byte
	lengthStart := 12
	if salted {
		if len(body) < 22 {
			return "", errors.New("happ: crypt5 salted header is too short")
		}
		salt = body[14:22]
		lengthStart = 22
	}

	lengthEnd := lengthStart
	for lengthEnd < len(body) && isASCIIDigit(body[lengthEnd]) {
		lengthEnd++
	}
	if lengthEnd == lengthStart {
		return "", errors.New("happ: crypt5 segment length is missing")
	}
	segmentLength, err := strconv.Atoi(string(body[lengthStart:lengthEnd]))
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 segment length is invalid: %w", err)
	}

	packed := body[lengthEnd:]
	if len(packed) == 0 || segmentLength < 1 || segmentLength > len(packed)-1 {
		return "", errors.New("happ: crypt5 segment is truncated")
	}

	encryptedURL := string(packed[1 : segmentLength+1])
	rsaCiphertext, err := base64Decode(string(packed[segmentLength+1:]))
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 key block is not valid base64: %w", err)
	}
	//nolint:staticcheck // happ 加密格式固定使用 PKCS#1 v1.5，换成 OAEP 将无法解密既有链接。
	rsaPlaintext, err := rsa.DecryptPKCS1v15(nil, key, rsaCiphertext)
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 key recovery failed: %w", err)
	}
	chachaKey, err := base64Decode(string(swapAdjacent(rsaPlaintext)))
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 key is not valid base64: %w", err)
	}
	if len(chachaKey) != 32 {
		return "", fmt.Errorf("happ: crypt5 key is %d bytes, expected 32", len(chachaKey))
	}
	if len(salt) > 0 {
		for i := range chachaKey {
			chachaKey[i] ^= salt[i%len(salt)]
		}
	}

	ciphertext, err := base64Decode(encryptedURL)
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 body is not valid base64: %w", err)
	}
	aead, err := chacha20poly1305.New(chachaKey)
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 cipher setup failed: %w", err)
	}
	intermediate, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 authentication failed: %w", err)
	}
	plaintext, err := base64Decode(string(swapAdjacent(intermediate)))
	if err != nil {
		return "", fmt.Errorf("happ: crypt5 result is not valid base64: %w", err)
	}
	return string(plaintext), nil
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

// swapBlockHalves turns ABCD into CDAB for every complete four byte block.
// The transform is its own inverse.
func swapBlockHalves(data []byte) []byte {
	result := make([]byte, len(data))
	copy(result, data)
	full := len(result) - len(result)%4
	for i := 0; i < full; i += 4 {
		result[i], result[i+2] = result[i+2], result[i]
		result[i+1], result[i+3] = result[i+3], result[i+1]
	}
	return result
}

// swapAdjacent turns ABCD into BADC for every byte pair.
func swapAdjacent(data []byte) []byte {
	result := make([]byte, len(data))
	copy(result, data)
	for i := 0; i+1 < len(result); i += 2 {
		result[i], result[i+1] = result[i+1], result[i]
	}
	return result
}
