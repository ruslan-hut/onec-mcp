package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"os"
)

// randomToken — криптостойкий 32-байтный токен в base64url без padding.
// Используется для client_id, auth_code, access_token, refresh_token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// verifyPKCE — проверка S256: BASE64URL(SHA256(verifier)) == challenge.
// plain method спецификация MCP не требует, поддерживаем только S256.
func verifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	calc := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(calc), []byte(challenge)) == 1
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o750)
}
