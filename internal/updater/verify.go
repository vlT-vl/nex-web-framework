package updater

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

func verifySignature(hash string, signature []byte, keys [][]byte) bool {
	digest, err := hex.DecodeString(normalizeHash(hash))
	if err != nil || len(digest) != 32 || len(signature) != ed25519.SignatureSize {
		return false
	}
	for _, key := range keys {
		if len(key) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(key), digest, signature) {
			return true
		}
	}
	return false
}

func decodeSignature(data []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(data))
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(decoded) == ed25519.SignatureSize {
		return decoded, nil
	}
	if len(data) == ed25519.SignatureSize {
		return data, nil
	}
	return nil, coded("signature_invalid", fmt.Errorf("signature encoding is invalid"))
}
