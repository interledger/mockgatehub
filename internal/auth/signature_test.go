package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateSignature_FullURL(t *testing.T) {
	timestamp := "1686040166173"
	method := "POST"
	url := "http://mockgatehub:8080/auth/v1/tokens?clientId=abc"
	body := `{"scope":["auth"]}`
	secret := "local-test-app-secret"

	message := fmt.Sprintf("%s|%s|%s|%s", timestamp, method, url, body)
	message = strings.Trim(message, "|")
	want := hmacSHA256Hex(message, secret)

	got := GenerateSignature(timestamp, method, url, body, secret)

	assert.Equal(t, want, got)
	assert.Len(t, got, 64)
}

func TestGenerateSignature_EmptyBody_TrimsPipe(t *testing.T) {
	timestamp := "1686040166173"
	method := "GET"
	url := "http://mockgatehub:8080/id/v1/users/123"
	body := ""
	secret := "local-test-app-secret"

	message := fmt.Sprintf("%s|%s|%s", timestamp, method, url)
	want := hmacSHA256Hex(message, secret)

	got := GenerateSignature(timestamp, method, url, body, secret)

	assert.Equal(t, want, got)
	assert.Len(t, got, 64)
}

func TestGenerateSignature_Deterministic(t *testing.T) {
	timestamp := "1686040166173"
	method := "POST"
	url := "http://mockgatehub:8080/auth/v1/tokens"
	body := `{"scope":["auth"]}`
	secret := "local-test-app-secret"

	sig1 := GenerateSignature(timestamp, method, url, body, secret)
	sig2 := GenerateSignature(timestamp, method, url, body, secret)

	assert.Equal(t, sig1, sig2)
}

func hmacSHA256Hex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
