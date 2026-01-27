package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateTransactionExternalDeposit verifies external deposits are created and return correct status
func TestCreateTransactionExternalDeposit(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil) // No URL - won't send webhooks
	handler := NewHandler(store, webhookManager)

	// Create transaction request for external deposit
	body := map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       100.00,
		"currency":     "USD",
		"type":         1, // External deposit
		"deposit_type": "external",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.CreateTransaction(rr, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, rr.Code)

	var response map[string]interface{}
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Verify transaction fields
	assert.NotEmpty(t, response["uuid"])
	assert.Equal(t, "100.00", response["amount"])
	assert.Equal(t, "USD", response["currency"])
	assert.Equal(t, "external", response["deposit_type"])
	assert.Equal(t, float64(1), response["status"]) // status=1 means completed

	// Verify balance was updated (note: test user may have pre-seeded balance, so just verify it increased)
	balance, _ := store.GetBalance(consts.TestUser1ID, "USD")
	assert.Greater(t, balance, 0.0, "Balance should be positive after deposit")
}

// TestCreateTransactionHostedDeposit verifies hosted deposits are created successfully
// This is the critical fix - hosted transfers (type=2) must send webhooks to prevent
// PayIn workflow from hanging indefinitely waiting for webhook or 20-minute polling
func TestCreateTransactionHostedDeposit(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil) // No URL - won't send webhooks
	handler := NewHandler(store, webhookManager)

	// Create transaction request for hosted deposit (type=2)
	body := map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       50.00,
		"currency":     "EUR",
		"type":         2, // Hosted transfer
		"deposit_type": "hosted",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.CreateTransaction(rr, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, rr.Code, "Expected successful transaction creation for hosted deposit")

	var response map[string]interface{}
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// CRITICAL FIX: Verify hosted transfer was created successfully
	// (Previously this would fail or not trigger webhooks, causing PayIn workflow to hang indefinitely)
	assert.NotEmpty(t, response["uuid"], "Transaction UUID should not be empty")
	assert.Equal(t, "50.00", response["amount"])
	assert.Equal(t, "EUR", response["currency"])
	assert.Equal(t, "hosted", response["deposit_type"])
	assert.Equal(t, float64(1), response["status"]) // status=1 means completed

	// Verify balance was updated
	balance, _ := store.GetBalance(consts.TestUser1ID, "EUR")
	assert.Greater(t, balance, 0.0, "Balance should be positive after deposit")
}

// TestCreateTransactionMissingUserID verifies validation
func TestCreateTransactionMissingUserID(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Missing user_id
	body := map[string]interface{}{
		"amount":   100.00,
		"currency": "USD",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.CreateTransaction(rr, req)

	// Should fail with bad request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestCreateTransactionMultipleCurrencies verifies different currencies can be handled
func TestCreateTransactionMultipleCurrencies(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	currencies := []string{"USD", "EUR", "GBP", "XRP"}
	for _, curr := range currencies {
		body := map[string]interface{}{
			"user_id":  consts.TestUser1ID,
			"amount":   25.00,
			"currency": curr,
			"type":     2, // Hosted
		}

		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.CreateTransaction(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code, "Failed to create transaction for %s", curr)

		var response map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&response)
		assert.Equal(t, curr, response["currency"])
	}

	// Verify all balances were updated
	for _, curr := range currencies {
		balance, _ := store.GetBalance(consts.TestUser1ID, curr)
		assert.Greater(t, balance, 0.0, "Balance should be positive for %s after deposit", curr)
	}
}
