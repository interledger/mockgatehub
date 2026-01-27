package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GenerateSignature generates an HMAC-SHA256 signature
// Format: HMAC-SHA256(timestamp + method + path + body, secret)
func GenerateSignature(timestamp, method, path, body, secret string) string {
	message := timestamp + method + path + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// GenerateGatehubSignature generates signature in Gatehub backend format
// Format: HMAC-SHA256(timestamp_ms|method|url|body, secret)
func GenerateGatehubSignature(timestamp, method, url, body, secret string) string {
	message := fmt.Sprintf("%s|%s|%s|%s", timestamp, method, url, body)
	message = strings.Trim(message, "|")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// NormalizeTimestamp converts milliseconds to seconds if needed
// Backend sends milliseconds (>= 1e10), but we validate in seconds
func NormalizeTimestamp(timestampStr string) (int64, error) {
	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp format")
	}

	// If timestamp is > 1e10, it's in milliseconds; convert to seconds
	if ts > 10000000000 {
		ts = ts / 1000
	}

	return ts, nil
}

// ValidateSignature validates an incoming request signature
func ValidateSignature(r *http.Request, secret string) (bool, error) {
	// Extract headers
	timestamp := r.Header.Get("x-gatehub-timestamp")
	signature := r.Header.Get("x-gatehub-signature")
	appID := r.Header.Get("x-gatehub-app-id")

	if timestamp == "" || signature == "" || appID == "" {
		return false, fmt.Errorf("missing required headers")
	}

	// Validate timestamp (allow 5 minute window). We normalize only for the range check,
	// but we keep the ORIGINAL timestamp string for signature validation because the sender
	// signed using that exact value (often milliseconds).
	normalizedTS, err := NormalizeTimestamp(timestamp)
	if err != nil {
		return false, fmt.Errorf("invalid timestamp format")
	}

	now := time.Now().Unix()
	if now-normalizedTS > 300 || normalizedTS-now > 300 {
		return false, fmt.Errorf("timestamp out of acceptable range")
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read body")
	}

	// Generate expected signature using ORIGINAL timestamp (not normalized) to match sender
	method := r.Method
	path := r.URL.Path
	expectedSig := GenerateSignature(timestamp, method, path, string(body), secret)

	// Compare signatures (constant time)
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return false, fmt.Errorf("signature mismatch")
	}

	return true, nil
}

// SignRequest adds signature headers to an outgoing request
func SignRequest(r *http.Request, secret string, body []byte) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	method := r.Method
	path := r.URL.Path

	signature := GenerateSignature(timestamp, method, path, string(body), secret)

	r.Header.Set("x-gatehub-timestamp", timestamp)
	r.Header.Set("x-gatehub-signature", signature)
	r.Header.Set("x-gatehub-app-id", "mockgatehub")
}

// GenerateGateHubWebhookSignature generates the signature for webhooks as expected by the backend
// The backend expects: HMAC-SHA256(json_body, hex_decoded_secret)
func GenerateGateHubWebhookSignature(jsonBody, hexSecret string) string {
	// Decode hex secret to bytes (the secret is stored as hex in env)
	key, err := hex.DecodeString(hexSecret)
	if err != nil {
		// If decoding fails, use the secret as-is (it might not be hex encoded)
		key = []byte(hexSecret)
	}

	// Create HMAC-SHA256 of the body
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(jsonBody))
	return hex.EncodeToString(mac.Sum(nil))
}
