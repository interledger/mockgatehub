package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
)

// Config holds application configuration
type Config struct {
	Port               string
	LogLevel           string
	RedisURL           string
	RedisDB            int
	WebhookURL         string
	WebhookSecret      string
	WebhookMinDelaySec float64
	// WebhookPollIntervalMS and WebhookBatchSize pace webhook delivery.
	// Together they bound throughput, so an environment expecting many
	// webhooks in quick succession should lower the interval.
	WebhookPollIntervalMS int
	WebhookBatchSize      int
	UseRedis              bool
	EnforceAuthentication bool
	ValidCredentials      map[string]string // appID -> secret
	DefaultOrganizationID string

	// PublicBaseURL is the externally reachable base URL of this mockgatehub
	// instance, without a trailing slash. It is used to build absolute URLs in
	// API responses that a browser follows directly rather than going back
	// through the calling backend — currently the card-data tokenisation link.
	PublicBaseURL string

	// AsyncWithdrawals switches withdrawals from settling immediately to
	// staying pending until an outcome is triggered, which is how a real
	// provider behaves. It defaults to off: a consumer that does not handle
	// withdrawal webhooks would see its withdrawals never complete.
	AsyncWithdrawals bool

	// CardDataTokenSecret is the HMAC secret used to sign the short-lived
	// card-data JWTs returned by POST /cards/v1/token/card-data. It is
	// deliberately not a compiled-in constant: the endpoint those tokens
	// unlock is not HMAC-authenticated, so a shared hard-coded key would let
	// anyone mint tokens. When unset we generate a random secret per process.
	CardDataTokenSecret string
}

// Load reads configuration from environment variables
func Load() *Config {
	cfg := &Config{
		Port:                  getEnv("MOCKGATEHUB_PORT", "8080"),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		RedisURL:              getEnv("MOCKGATEHUB_REDIS_URL", ""),
		RedisDB:               getEnvInt("MOCKGATEHUB_REDIS_DB", 0),
		WebhookURL:            getEnv("WEBHOOK_URL", ""),
		WebhookSecret:         getEnv("WEBHOOK_SECRET", "mock-secret"),
		WebhookMinDelaySec:    getEnvFloat("WEBHOOK_MIN_DELAY_SEC", 0.05),
		EnforceAuthentication: getEnvBool("MOCKGATEHUB_ENFORCE_AUTHENTICATION", true),
		ValidCredentials:      parseCredentials(getEnv("MOCKGATEHUB_VALID_CREDENTIALS", "local-test-app-id:local-test-app-secret")),
		DefaultOrganizationID: getEnv("DEFAULT_ORGANIZATION_ID", "default-org"),
		PublicBaseURL:         getEnv("MOCKGATEHUB_PUBLIC_BASE_URL", "http://localhost:8080"),
		CardDataTokenSecret:   getEnv("MOCKGATEHUB_CARD_DATA_TOKEN_SECRET", ""),
		AsyncWithdrawals:      getEnvBool("MOCKGATEHUB_ASYNC_WITHDRAWALS", false),
	}

	// The 2-second minimum delay clamp was removed upstream so webhooks can be
	// delivered promptly; the value is used as configured.

	// Callers concatenate PublicBaseURL with rooted paths, so a trailing
	// slash would produce a double slash in the generated URL.
	cfg.PublicBaseURL = strings.TrimRight(cfg.PublicBaseURL, "/")

	if cfg.CardDataTokenSecret == "" {
		cfg.CardDataTokenSecret = randomSecret(32)
	}

	// Use Redis if URL is provided
	cfg.UseRedis = cfg.RedisURL != ""

	return cfg
}

// randomSecret returns a URL-safe base64 encoding of nBytes of cryptographically
// random data. If the system RNG is unavailable we still return a non-empty
// value: an unguessable secret is preferable, but an empty signing key would
// silently accept every forged token.
func randomSecret(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "mockgatehub-insecure-fallback-card-data-secret"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// getEnv gets environment variable with fallback
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt gets integer environment variable with fallback
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// getEnvFloat gets float64 environment variable with fallback
func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}

// getEnvBool gets boolean environment variable with fallback
func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1" || val == "yes"
	}
	return defaultVal
}

// parseCredentials parses credentials from format: "appId1:secret1,appId2:secret2"
func parseCredentials(credStr string) map[string]string {
	creds := make(map[string]string)
	if credStr == "" {
		return creds
	}

	for _, pair := range splitString(credStr, ',') {
		parts := splitString(pair, ':')
		if len(parts) == 2 {
			creds[parts[0]] = parts[1]
		}
	}
	return creds
}

// splitString splits a string by delimiter (helper for parsing)
func splitString(s string, delim byte) []string {
	var result []string
	var current []byte
	for i := 0; i < len(s); i++ {
		if s[i] == delim {
			result = append(result, string(current))
			current = []byte{}
		} else {
			current = append(current, s[i])
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}
