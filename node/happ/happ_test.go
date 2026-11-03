package happ

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// crypt5ExampleLink is the public test vector from the happ-decryptor project.
const crypt5ExampleLink = "happ://crypt5/fzvdO4bMOfTWNaB3taWRhRaF64soexaE9dm3ZlLK0Rke9Rz3BG1f9gmj4tSDpjRSWWdX5G6oaaQK9Gs+fdJPWNXIF08BsaeifWtfTlCvC/nSWDv0ZrofgJXkQ8MlUk63CoJkt7RvXAfablYXb/cYWEZJQkDMyMaE5/1KegAbWVWVI60MPqDylUyYoLtOeOOX9amvELecOZ4kKz1QVqgE9uCBz3py+3Ghr1iVGKOhFwb98OFP+j0tGvDo/3d609DVq3RwBGXu1ogZ7PTc3/A5IlaA3Hff5IlVujozQ3ywmQBsTGd+l3AHJAX1oDbPkRSSwg7Y7hl3AKXKpZsEhMzPbJY8UxZ7GmVsxeROLopVx85ACqakzg+ZZwdZslfKgdRzUmL9Mv895HDOHE3tbh6qnDhE9Ew/Epx1iBCb2HjorOLDBluH8ztdL9mdUX+turjC4GLN0YR55P3H23A0W5zl0di5YfrI2nUBKxh30lUVG9NbqYKlwxgmhxAUZrQ6cnFGF2VuZ9VJLQ3sQ8rqXTtfau8ySbu770Hd6vVEun8aSJm4W4M0DagKbORL4A4M6Cuf1v/jj7EWhA9yhhcSuxkc6WUWMGYRraWBM9vSrSbjeT1U69a3n9T56M/TOJWf4z8fXrHRnCR1tfzwrjHiOJfZGiKvbAd4k6f+VADYpLdCq+ornEElv7V0sByfwPTgep+Q33Qkl67ArHpmbZDcCyWkGz0BoUzGJFe58YiS/oNFVdufbuDnFd1ArAVMztJJJbxlo4Is48+Iof=ff"

func TestDecryptCrypt5(t *testing.T) {
	got, err := Decrypt(crypt5ExampleLink)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got != "https://example.com/sub" {
		t.Fatalf("Decrypt() = %q, want %q", got, "https://example.com/sub")
	}
}

func TestDecryptCrypt5RejectsTamperedPayload(t *testing.T) {
	const prefix = "happ://crypt5/"
	tampered := []byte(crypt5ExampleLink)
	index := len(prefix) + 200
	if tampered[index] == 'A' {
		tampered[index] = 'B'
	} else {
		tampered[index] = 'A'
	}

	if _, err := Decrypt(string(tampered)); err == nil {
		t.Fatal("Decrypt() accepted a tampered crypt5 payload")
	}
}

func TestDecryptLegacyGenerations(t *testing.T) {
	cases := []struct {
		name    string
		ordinal int
		url     string
	}{
		{name: "crypt", ordinal: 0, url: "https://example.com/sub"},
		{name: "crypt2", ordinal: 1, url: "https://example.com/sub?plan=pro&region=eu&token=" + strings.Repeat("a", 600)},
		{name: "crypt3", ordinal: 2, url: "https://example.com/sub?plan=pro&region=eu&token=" + strings.Repeat("b", 600)},
		{name: "crypt4", ordinal: 3, url: "https://example.com/sub?plan=pro&region=eu&token=" + strings.Repeat("c", 600)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := legacyKey(tc.ordinal)
			if err != nil {
				t.Fatalf("legacyKey(%d) error = %v", tc.ordinal, err)
			}

			payload := encryptLegacy(t, &key.PublicKey, tc.url)
			got, err := Decrypt("happ://" + tc.name + "/" + payload)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if got != tc.url {
				t.Fatalf("Decrypt() returned %d bytes, want %d bytes", len(got), len(tc.url))
			}
		})
	}
}

func TestDecryptRejectsBrokenInput(t *testing.T) {
	if IsLink("https://example.com/sub") {
		t.Error("IsLink() accepted a plain URL")
	}
	if IsLink("HAPP://crypt5/abc") != true {
		t.Error("IsLink() must be case insensitive")
	}

	if _, err := Decrypt("https://example.com/sub"); !errors.Is(err, ErrNotALink) {
		t.Errorf("Decrypt() error = %v, want ErrNotALink", err)
	}
	if _, err := Decrypt("happ://unknown/abc"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Decrypt() error = %v, want ErrUnsupported", err)
	}
	if _, err := Decrypt("happ://crypt5"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Decrypt() error = %v, want ErrUnsupported", err)
	}
	if _, err := Decrypt("happ://crypt/not-base64!!"); err == nil {
		t.Error("Decrypt() accepted an invalid base64 payload")
	}
	if _, err := Decrypt("happ://crypt5/AAAA"); err == nil {
		t.Error("Decrypt() accepted a truncated crypt5 payload")
	}
}

func TestGeneration(t *testing.T) {
	if got := Generation("happ://crypt5/abc"); got != "crypt5" {
		t.Errorf("Generation() = %q, want %q", got, "crypt5")
	}
	if got := Generation(" HAPP://crypt2/abc"); got != "crypt2" {
		t.Errorf("Generation() = %q, want %q", got, "crypt2")
	}
	if got := Generation("happ://crypt/abc"); got != "crypt" {
		t.Errorf("Generation() = %q, want %q", got, "crypt")
	}
	if got := Generation("https://example.com/sub"); got != "" {
		t.Errorf("Generation() = %q, want an empty string", got)
	}
	if got := Generation("happ://"); got != "" {
		t.Errorf("Generation() = %q, want an empty string", got)
	}
}

func encryptLegacy(t *testing.T, publicKey *rsa.PublicKey, plaintext string) string {
	t.Helper()

	chunkSize := publicKey.Size() - 11
	encrypted := make([]byte, 0, publicKey.Size())
	for offset := 0; offset < len(plaintext); offset += chunkSize {
		end := offset + chunkSize
		if end > len(plaintext) {
			end = len(plaintext)
		}
		//nolint:staticcheck // 测试需要按 happ 既有的 PKCS#1 v1.5 格式生成密文。
		block, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, []byte(plaintext[offset:end]))
		if err != nil {
			t.Fatalf("EncryptPKCS1v15() error = %v", err)
		}
		encrypted = append(encrypted, block...)
	}
	return base64.RawURLEncoding.EncodeToString(encrypted)
}
