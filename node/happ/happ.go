// Package happ decodes happ:// encrypted deep links into the plaintext URL they carry.
//
// Supported generations: crypt, crypt2, crypt3, crypt4 (RSA PKCS#1 v1.5) and
// crypt5 (RSA key recovery plus ChaCha20-Poly1305). Everything is decrypted
// locally: no network calls are made, nothing is reported anywhere and the
// private keys never leave the binary.
package happ

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	linkScheme    = "happ://"
	maxLinkLength = 64 << 10
)

// ErrNotALink is returned when the value does not use the happ:// scheme.
var ErrNotALink = errors.New("not a happ:// link")

// ErrUnsupported is returned for link generations this package cannot decode.
var ErrUnsupported = errors.New("unsupported happ:// link type")

// IsLink reports whether the value looks like a happ:// deep link.
func IsLink(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), linkScheme)
}

// Generation returns the link type, for example "crypt5", or an empty string
// when the value is not a happ link.
func Generation(value string) string {
	trimmed := strings.TrimSpace(value)
	if !IsLink(trimmed) {
		return ""
	}
	path := trimmed[len(linkScheme):]
	if idx := strings.Index(path, "/"); idx >= 0 {
		return strings.ToLower(path[:idx])
	}
	return strings.ToLower(path)
}

// Decrypt turns a happ:// link into the plaintext URL it protects.
func Decrypt(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !IsLink(trimmed) {
		return "", ErrNotALink
	}
	if len(trimmed) > maxLinkLength {
		return "", fmt.Errorf("happ: link is longer than %d bytes", maxLinkLength)
	}

	path := trimmed[len(linkScheme):]
	slash := strings.Index(path, "/")
	if slash < 0 {
		return "", fmt.Errorf("%w: %q has no payload", ErrUnsupported, Generation(trimmed))
	}
	kind := strings.ToLower(path[:slash])
	payload := strings.TrimSpace(path[slash+1:])
	if payload == "" {
		return "", fmt.Errorf("%w: %q has an empty payload", ErrUnsupported, kind)
	}

	switch kind {
	case "crypt":
		return decryptLegacy(0, payload)
	case "crypt2":
		return decryptLegacy(1, payload)
	case "crypt3":
		return decryptLegacy(2, payload)
	case "crypt4":
		return decryptLegacy(3, payload)
	case "crypt5":
		return decryptCrypt5(payload)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupported, kind)
	}
}

// base64Decode accepts standard and URL-safe base64, ignores whitespace and
// tolerates missing padding, the same way the reference implementation does.
func base64Decode(value string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, value)
	cleaned = strings.TrimRight(cleaned, "=")
	if cleaned == "" {
		return nil, errors.New("empty base64 payload")
	}

	switch len(cleaned) % 4 {
	case 2:
		cleaned += "=="
	case 3:
		cleaned += "="
	}
	return base64.StdEncoding.DecodeString(cleaned)
}
