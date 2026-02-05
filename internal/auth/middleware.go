package auth

import (
	"bytes"
	"io"
	"net/http"

	"mockgatehub/internal/logger"
	"go.uber.org/zap"
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
				logger.Error("auth failure: missing authentication headers",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("app_id", appID),
					zap.String("timestamp", timestamp),
					zap.String("signature", signature),
				)
				http.Error(w, `{"error": {"status_code": 401, "status": "Unauthorized"}}`, http.StatusUnauthorized)
				return
			}

			// Check if app is registered
			secret, exists := validCredentials[appID]
			if !exists {
				logger.Error("auth failure: unknown app id",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("received_app_id", appID),
					zap.Strings("registered_app_ids", getRegisteredAppIDs(validCredentials)),
				)
				http.Error(w, `{"error": {"status_code": 401, "status": "Unauthorized"}}`, http.StatusUnauthorized)
				return
			}

			// Read and preserve request body
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				logger.Error("auth failure: failed to read request body",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("app_id", appID),
					zap.Error(err),
				)
				http.Error(w, `{"error": {"status_code": 400, "status": "Bad Request"}}`, http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// Validate signature using ORIGINAL timestamp (do NOT normalize for signature check)
			// Signature was generated with the timestamp as sent, so we must validate with the same timestamp
			valid, err := ValidateSignatureWithAppID(r, secret, appID, timestamp, signature, string(bodyBytes))
			if !valid {
				// Log detailed signature validation failure including secrets
				expectedSimpleSig := GenerateSignature(timestamp, r.Method, r.URL.Path, string(bodyBytes), secret)
				fullURL := r.URL.Path
				if r.URL.RawQuery != "" {
					fullURL = r.URL.Path + "?" + r.URL.RawQuery
				}
				expectedGatehubSig := GenerateGatehubSignature(timestamp, r.Method, fullURL, string(bodyBytes), secret)

				logger.Error("auth failure: signature mismatch",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("app_id", appID),
					zap.String("received_signature", signature),
					zap.String("expected_signature_simple", expectedSimpleSig),
					zap.String("expected_signature_gatehub", expectedGatehubSig),
					zap.String("secret", secret),
					zap.String("timestamp", timestamp),
					zap.String("body", string(bodyBytes)),
					zap.Error(err),
				)
				http.Error(w, `{"error": {"status_code": 401, "status": "Unauthorized"}}`, http.StatusUnauthorized)
				return
			}

			logger.Info("auth success",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("app_id", appID),
			)
			next.ServeHTTP(w, r)
		})
	}
}

// getRegisteredAppIDs returns a list of registered app IDs (for logging purposes)
func getRegisteredAppIDs(validCredentials map[string]string) []string {
	appIDs := make([]string, 0, len(validCredentials))
	for appID := range validCredentials {
		appIDs = append(appIDs, appID)
	}
	return appIDs
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
