package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env vars to test defaults (t.Setenv registers cleanup to restore originals)
	for _, key := range []string{
		"MOCKGATEHUB_PORT", "LOG_LEVEL", "MOCKGATEHUB_REDIS_URL", "MOCKGATEHUB_REDIS_DB",
		"WEBHOOK_URL", "WEBHOOK_SECRET", "WEBHOOK_MIN_DELAY_SEC",
		"MOCKGATEHUB_ENFORCE_AUTHENTICATION", "MOCKGATEHUB_VALID_CREDENTIALS",
		"DEFAULT_ORGANIZATION_ID",
	} {
		t.Setenv(key, "")
	}

	cfg := Load()

	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "", cfg.RedisURL)
	assert.Equal(t, 0, cfg.RedisDB)
	assert.Equal(t, "", cfg.WebhookURL)
	assert.Equal(t, "mock-secret", cfg.WebhookSecret)
	assert.Equal(t, 0.05, cfg.WebhookMinDelaySec, "default min delay should be 0.05")
	assert.True(t, cfg.EnforceAuthentication)
	assert.False(t, cfg.UseRedis)
	assert.Equal(t, "default-org", cfg.DefaultOrganizationID)
	assert.Equal(t, "local-test-app-secret", cfg.ValidCredentials["local-test-app-id"])
}

func TestLoad_CustomEnvVars(t *testing.T) {
	t.Setenv("MOCKGATEHUB_PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("MOCKGATEHUB_REDIS_URL", "redis://localhost:6379")
	t.Setenv("MOCKGATEHUB_REDIS_DB", "3")
	t.Setenv("WEBHOOK_URL", "http://example.com/hooks")
	t.Setenv("WEBHOOK_SECRET", "my-secret")
	t.Setenv("WEBHOOK_MIN_DELAY_SEC", "5.0")
	t.Setenv("MOCKGATEHUB_ENFORCE_AUTHENTICATION", "false")
	t.Setenv("MOCKGATEHUB_VALID_CREDENTIALS", "app1:secret1,app2:secret2")
	t.Setenv("DEFAULT_ORGANIZATION_ID", "my-org")

	cfg := Load()

	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "redis://localhost:6379", cfg.RedisURL)
	assert.Equal(t, 3, cfg.RedisDB)
	assert.Equal(t, "http://example.com/hooks", cfg.WebhookURL)
	assert.Equal(t, "my-secret", cfg.WebhookSecret)
	assert.Equal(t, 5.0, cfg.WebhookMinDelaySec)
	assert.False(t, cfg.EnforceAuthentication)
	assert.True(t, cfg.UseRedis)
	assert.Equal(t, "my-org", cfg.DefaultOrganizationID)
	assert.Equal(t, "secret1", cfg.ValidCredentials["app1"])
	assert.Equal(t, "secret2", cfg.ValidCredentials["app2"])
}

func TestLoad_WebhookMinDelayClamp(t *testing.T) {
	t.Setenv("WEBHOOK_MIN_DELAY_SEC", "0.5")
	cfg := Load()
	assert.Equal(t, 0.5, cfg.WebhookMinDelaySec, "should use value as-is without clamping")
}

func TestGetEnv_Default(t *testing.T) {
	t.Setenv("TEST_NONEXISTENT_KEY", "")
	assert.Equal(t, "fallback", getEnv("TEST_NONEXISTENT_KEY", "fallback"))
}

func TestGetEnv_Set(t *testing.T) {
	t.Setenv("TEST_KEY", "value")
	assert.Equal(t, "value", getEnv("TEST_KEY", "fallback"))
}

func TestGetEnvInt_Default(t *testing.T) {
	t.Setenv("TEST_INT_KEY", "")
	assert.Equal(t, 42, getEnvInt("TEST_INT_KEY", 42))
}

func TestGetEnvInt_Valid(t *testing.T) {
	t.Setenv("TEST_INT_KEY", "7")
	assert.Equal(t, 7, getEnvInt("TEST_INT_KEY", 0))
}

func TestGetEnvInt_Invalid(t *testing.T) {
	t.Setenv("TEST_INT_KEY", "not-a-number")
	assert.Equal(t, 99, getEnvInt("TEST_INT_KEY", 99))
}

func TestGetEnvFloat_Default(t *testing.T) {
	t.Setenv("TEST_FLOAT_KEY", "")
	assert.Equal(t, 1.5, getEnvFloat("TEST_FLOAT_KEY", 1.5))
}

func TestGetEnvFloat_Valid(t *testing.T) {
	t.Setenv("TEST_FLOAT_KEY", "3.14")
	assert.InDelta(t, 3.14, getEnvFloat("TEST_FLOAT_KEY", 0), 0.001)
}

func TestGetEnvFloat_Invalid(t *testing.T) {
	t.Setenv("TEST_FLOAT_KEY", "abc")
	assert.Equal(t, 2.0, getEnvFloat("TEST_FLOAT_KEY", 2.0))
}

func TestGetEnvBool_Variations(t *testing.T) {
	for _, val := range []string{"true", "1", "yes"} {
		t.Setenv("TEST_BOOL_KEY", val)
		assert.True(t, getEnvBool("TEST_BOOL_KEY", false), "expected true for %q", val)
	}

	for _, val := range []string{"false", "0", "no", "anything"} {
		t.Setenv("TEST_BOOL_KEY", val)
		assert.False(t, getEnvBool("TEST_BOOL_KEY", true), "expected false for %q", val)
	}

	t.Setenv("TEST_BOOL_KEY", "")
	assert.True(t, getEnvBool("TEST_BOOL_KEY", true))
	assert.False(t, getEnvBool("TEST_BOOL_KEY", false))
}

func TestParseCredentials(t *testing.T) {
	assert.Empty(t, parseCredentials(""))
	assert.Equal(t, map[string]string{"a": "b"}, parseCredentials("a:b"))
	assert.Equal(t, map[string]string{"a": "b", "c": "d"}, parseCredentials("a:b,c:d"))
	// Malformed (no colon) should be skipped
	result := parseCredentials("nocolon,a:b")
	assert.Equal(t, "b", result["a"])
	assert.NotContains(t, result, "nocolon")
}

func TestSplitString(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, splitString("a,b,c", ','))
	assert.Equal(t, []string{"single"}, splitString("single", ','))
	assert.Equal(t, []string{"a", "b"}, splitString("a:b", ':'))
}

func TestLoad_PublicBaseURLDefault(t *testing.T) {
	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "")
	t.Setenv("MOCKGATEHUB_PORT", "")
	cfg := Load()
	assert.Equal(t, "http://localhost:8080", cfg.PublicBaseURL)
}

func TestLoad_PublicBaseURLFollowsThePort(t *testing.T) {
	// The links built from this are followed by a browser. Defaulting to 8080
	// while the server listens on 9090 would point them at nothing.
	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "")
	t.Setenv("MOCKGATEHUB_PORT", "9090")

	cfg := Load()

	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "http://localhost:9090", cfg.PublicBaseURL)
}

func TestLoad_ExplicitPublicBaseURLWinsOverThePort(t *testing.T) {
	// Behind a proxy the externally reachable URL has nothing to do with the
	// port the process binds.
	t.Setenv("MOCKGATEHUB_PORT", "9090")
	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "https://mock.example.com")

	assert.Equal(t, "https://mock.example.com", Load().PublicBaseURL)
}

func TestLoad_PublicBaseURLTrimsTrailingSlash(t *testing.T) {
	// Callers build absolute URLs by concatenating a rooted path, so a trailing
	// slash would yield "https://host//cards/v1/...".
	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "https://mock.example.com/")
	cfg := Load()
	assert.Equal(t, "https://mock.example.com", cfg.PublicBaseURL)

	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "https://mock.example.com///")
	cfg = Load()
	assert.Equal(t, "https://mock.example.com", cfg.PublicBaseURL)
}

func TestLoad_CardDataTokenSecretGeneratedWhenUnset(t *testing.T) {
	t.Setenv("MOCKGATEHUB_CARD_DATA_TOKEN_SECRET", "")

	first := Load()
	second := Load()

	assert.NotEmpty(t, first.CardDataTokenSecret)
	// A compiled-in default would let anyone mint a card-data token, so each
	// load must produce its own secret.
	assert.NotEqual(t, first.CardDataTokenSecret, second.CardDataTokenSecret,
		"generated secret must not be a fixed value")
}

func TestLoad_CardDataTokenSecretHonoursEnv(t *testing.T) {
	t.Setenv("MOCKGATEHUB_CARD_DATA_TOKEN_SECRET", "explicit-secret")
	cfg := Load()
	assert.Equal(t, "explicit-secret", cfg.CardDataTokenSecret)
}

func TestRandomSecret_LengthAndUniqueness(t *testing.T) {
	a := randomSecret(32)
	b := randomSecret(32)
	assert.NotEmpty(t, a)
	assert.NotEqual(t, a, b)
	// base64 raw-url of 32 bytes is 43 chars.
	assert.Len(t, a, 43)
}

func TestLoad_AsyncWithdrawalsDefaultsOff(t *testing.T) {
	// Consumers that handle no withdrawal webhooks would see their withdrawals
	// never complete, so the asynchronous behaviour must be opt-in.
	t.Setenv("MOCKGATEHUB_ASYNC_WITHDRAWALS", "")
	assert.False(t, Load().AsyncWithdrawals)
}

func TestLoad_AsyncWithdrawalsIsOptIn(t *testing.T) {
	for _, truthy := range []string{"true", "1", "yes"} {
		t.Setenv("MOCKGATEHUB_ASYNC_WITHDRAWALS", truthy)
		assert.True(t, Load().AsyncWithdrawals, "expected %q to enable", truthy)
	}
	for _, falsy := range []string{"false", "0", "no"} {
		t.Setenv("MOCKGATEHUB_ASYNC_WITHDRAWALS", falsy)
		assert.False(t, Load().AsyncWithdrawals, "expected %q to disable", falsy)
	}
}
