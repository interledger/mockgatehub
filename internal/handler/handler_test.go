package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateMaskedPan verifies masked PAN format is numeric-only with correct structure
func TestGenerateMaskedPan(t *testing.T) {
	panPattern := regexp.MustCompile(`^\d{6}\*{6}\d{4}$`)

	for i := 0; i < 100; i++ {
		pan := generateMaskedPan()
		assert.Regexp(t, panPattern, pan, "masked PAN should match dddddd******dddd format, got: %s", pan)
	}
}

// TestRandomDigits verifies randomDigits returns only decimal digits of correct length
func TestRandomDigits(t *testing.T) {
	for _, n := range []int{1, 4, 6, 10} {
		digits := randomDigits(n)
		assert.Len(t, digits, n)
		assert.Regexp(t, regexp.MustCompile(`^\d+$`), digits, "randomDigits(%d) should be all numeric, got: %s", n, digits)
	}
}

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

// TestTransactionCompleteHandlerMissingAmount verifies that missing amount returns 400
func TestTransactionCompleteHandlerMissingAmount(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Create a wallet first
	createTestWallet(t, store, consts.TestUser1ID)

	// Request body missing amount field
	body := map[string]interface{}{
		"currency": "USD",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Map token to user
	handler.tokenToUser.Store("test-token", consts.TestUser1ID)

	rr := httptest.NewRecorder()
	handler.TransactionCompleteHandler(rr, req)

	// Should fail with bad request - amount is required
	assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected 400 Bad Request when amount is missing")
}

// TestTransactionCompleteHandlerMissingCurrency verifies that missing currency returns 400
func TestTransactionCompleteHandlerMissingCurrency(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Create a wallet first
	createTestWallet(t, store, consts.TestUser1ID)

	// Request body missing currency field
	body := map[string]interface{}{
		"amount": "100.00",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Map token to user
	handler.tokenToUser.Store("test-token", consts.TestUser1ID)

	rr := httptest.NewRecorder()
	handler.TransactionCompleteHandler(rr, req)

	// Should fail with bad request - currency is required
	assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected 400 Bad Request when currency is missing")
}

// TestTransactionCompleteHandlerEmptyBody verifies that empty body returns 400
func TestTransactionCompleteHandlerEmptyBody(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Create a wallet first
	createTestWallet(t, store, consts.TestUser1ID)

	// Empty body
	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader([]byte("")))
	req.Header.Set("Content-Type", "application/json")

	// Map token to user
	handler.tokenToUser.Store("test-token", consts.TestUser1ID)

	rr := httptest.NewRecorder()
	handler.TransactionCompleteHandler(rr, req)

	// Should fail with bad request - both amount and currency are missing
	assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected 400 Bad Request when body is empty")
}

// TestTransactionCompleteHandlerInvalidAmount verifies that non-numeric amount returns 400
func TestTransactionCompleteHandlerInvalidAmount(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Create a wallet first
	createTestWallet(t, store, consts.TestUser1ID)

	// Invalid amount (not a number)
	body := map[string]interface{}{
		"amount":   "not-a-number",
		"currency": "USD",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Map token to user
	handler.tokenToUser.Store("test-token", consts.TestUser1ID)

	rr := httptest.NewRecorder()
	handler.TransactionCompleteHandler(rr, req)

	// Should fail with bad request - amount is invalid
	assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected 400 Bad Request when amount is not numeric")
}

// TestTransactionCompleteHandlerInvalidCurrency verifies that unknown currency returns 400
func TestTransactionCompleteHandlerInvalidCurrency(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Create a wallet first
	createTestWallet(t, store, consts.TestUser1ID)

	// Invalid currency
	body := map[string]interface{}{
		"amount":   "100.00",
		"currency": "UNKNOWN",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Map token to user
	handler.tokenToUser.Store("test-token", consts.TestUser1ID)

	rr := httptest.NewRecorder()
	handler.TransactionCompleteHandler(rr, req)

	// Should fail with bad request - currency is unknown
	assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected 400 Bad Request when currency is unknown")
}

// TestTransactionCompleteHandlerValid verifies that valid request succeeds
func TestTransactionCompleteHandlerValid(t *testing.T) {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)

	webhookManager := webhook.NewManager("", "test-secret", nil)
	handler := NewHandler(store, webhookManager)

	// Create a wallet first
	createTestWallet(t, store, consts.TestUser1ID)

	// Valid request
	body := map[string]interface{}{
		"amount":   "100.00",
		"currency": "USD",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Map token to user
	handler.tokenToUser.Store("test-token", consts.TestUser1ID)

	rr := httptest.NewRecorder()
	handler.TransactionCompleteHandler(rr, req)

	// Should succeed
	assert.Equal(t, http.StatusOK, rr.Code, "Expected 200 OK for valid transaction complete request")

	var response map[string]interface{}
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "success", response["status"])
}

// Helper function to create a test wallet
func createTestWallet(t *testing.T, store storage.Storage, userID string) {
	wallet := &models.Wallet{
		UserID:    userID,
		Name:      "Test Wallet",
		Address:   "rN7n7otQDd6FczFgLdSqtcsAUxDkw6fzRH",
		Type:      0,
		Network:   30, // XRP Ledger
		CreatedAt: time.Now(),
	}
	err := store.CreateWallet(wallet)
	require.NoError(t, err)
}
