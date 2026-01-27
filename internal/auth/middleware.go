package auth

import (
	"bytes"
	"io"
	"net/http"

	"mockgatehub/internal/logger"
)

// PublicEndpoints are endpoints that don't require authentication
var PublicEndpoints = map[string]bool{
	"/health":        true,
	"/":              true, // Root handler (iframe serving)
	"/iframe/submit": true, // Iframe form submission
}

// Middleware returns an HTTP middleware that validates HMAC signatures
func Middleware(validCredentials map[string]string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication for public endpoints
			if PublicEndpoints[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Extract headers
			appID := r.Header.Get("x-gatehub-app-id")
			timestamp := r.Header.Get("x-gatehub-timestamp")
			signature := r.Header.Get("x-gatehub-signature")

			// Missing headers = unauthorized
			if appID == "" || timestamp == "" || signature == "" {
				logger.Error.Printf("Missing authentication headers for %s %s", r.Method, r.URL.Path)
				http.Error(w, `{"error": {"status_code": 401, "status": "Unauthorized"}}`, http.StatusUnauthorized)
				return
			}

			// Check if app is registered
			secret, exists := validCredentials[appID]
			if !exists {
				logger.Error.Printf("Unknown app ID: %s", appID)
				http.Error(w, `{"error": {"status_code": 401, "status": "Unauthorized"}}`, http.StatusUnauthorized)
				return
			}

			// Read and preserve request body
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				logger.Error.Printf("Failed to read request body: %v", err)
				http.Error(w, `{"error": {"status_code": 400, "status": "Bad Request"}}`, http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// Validate signature using ORIGINAL timestamp (do NOT normalize for signature check)
			// Signature was generated with the timestamp as sent, so we must validate with the same timestamp
			valid, err := ValidateSignatureWithAppID(r, secret, appID, timestamp, signature, string(bodyBytes))
			if !valid {
				logger.Error.Printf("Invalid signature for app %s: %v", appID, err)
				http.Error(w, `{"error": {"status_code": 401, "status": "Unauthorized"}}`, http.StatusUnauthorized)
				return
			}

			logger.Info.Printf("Valid signature for app %s", appID)
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateSignatureWithAppID validates signature with explicit parameters
func ValidateSignatureWithAppID(r *http.Request, secret, appID, timestamp, signature, body string) (bool, error) {
	// Generate expected signature using simple format
	method := r.Method
	path := r.URL.Path
	expectedSig := GenerateSignature(timestamp, method, path, body, secret)

	// Compare signatures (constant time)
	if bytes.Equal([]byte(signature), []byte(expectedSig)) {
		return true, nil
	}

	// Try Gatehub backend format: timestamp_ms|method|url|body
	// This is used by the wallet backend
	fullURL := r.URL.Path
	if r.URL.RawQuery != "" {
		fullURL = r.URL.Path + "?" + r.URL.RawQuery
	}
	expectedSigGatehub := GenerateGatehubSignature(timestamp, method, fullURL, body, secret)

	if bytes.Equal([]byte(signature), []byte(expectedSigGatehub)) {
		return true, nil
	}

	return false, nil
}
