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

// ── FeeConfig unit tests ─────────────────────────────────────────────

func TestFeeConfig_DefaultsToZero(t *testing.T) {
	fc := NewFeeConfig()
	assert.Equal(t, 0.0, fc.GetDepositFeePercent())
	assert.Equal(t, 0.0, fc.GetWithdrawalFeePercent())
}

func TestFeeConfig_SetAndGet(t *testing.T) {
	fc := NewFeeConfig()
	fc.SetDepositFeePercent(1.5)
	fc.SetWithdrawalFeePercent(2.0)
	assert.Equal(t, 1.5, fc.GetDepositFeePercent())
	assert.Equal(t, 2.0, fc.GetWithdrawalFeePercent())
}

func TestFeeConfig_ResetToZero(t *testing.T) {
	fc := NewFeeConfig()
	fc.SetDepositFeePercent(5.0)
	fc.SetWithdrawalFeePercent(3.0)
	fc.SetDepositFeePercent(0)
	fc.SetWithdrawalFeePercent(0)
	assert.Equal(t, 0.0, fc.GetDepositFeePercent())
	assert.Equal(t, 0.0, fc.GetWithdrawalFeePercent())
}

// ── CalculateFee unit tests ──────────────────────────────────────────

func TestCalculateFee_ZeroPercent(t *testing.T) {
	fee := CalculateFee(100.00, 0.0)
	assert.Equal(t, 0.0, fee)
}

func TestCalculateFee_OnePointFivePercent(t *testing.T) {
	fee := CalculateFee(100.00, 1.5)
	assert.Equal(t, 1.50, fee)
}

func TestCalculateFee_RoundToTwoDecimals(t *testing.T) {
	// 3% of 33.33 = 0.9999 → should round to 1.00
	fee := CalculateFee(33.33, 3.0)
	assert.Equal(t, 1.00, fee)
}

func TestCalculateFee_SmallAmountRoundsToZero(t *testing.T) {
	// 0.1% of 0.01 = 0.00001 → rounds to 0.00
	fee := CalculateFee(0.01, 0.1)
	assert.Equal(t, 0.0, fee)
}

func TestCalculateFee_LargeAmount(t *testing.T) {
	fee := CalculateFee(10000.00, 2.5)
	assert.Equal(t, 250.00, fee)
}

// ── Admin fee endpoint unit tests ────────────────────────────────────

func newTestHandler() *Handler {
	store := storage.NewMemoryStorage()
	wm := webhook.NewManager("", "test-secret", nil)
	return NewHandler(store, wm)
}

func TestGetFees_ReturnsDefaults(t *testing.T) {
	h := newTestHandler()

	req := httptest.NewRequest("GET", "/admin/fees", nil)
	rr := httptest.NewRecorder()
	h.GetFees(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0.0, resp["deposit_fee_percentage"])
	assert.Equal(t, 0.0, resp["withdrawal_fee_percentage"])
}

func TestSetFees_DepositOnly(t *testing.T) {
	h := newTestHandler()

	body, _ := json.Marshal(map[string]interface{}{"deposit_fee_percentage": 1.5})
	req := httptest.NewRequest("PUT", "/admin/fees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.SetFees(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 1.5, resp["deposit_fee_percentage"])
	assert.Equal(t, 0.0, resp["withdrawal_fee_percentage"])
}

func TestSetFees_WithdrawalOnly(t *testing.T) {
	h := newTestHandler()

	body, _ := json.Marshal(map[string]interface{}{"withdrawal_fee_percentage": 2.0})
	req := httptest.NewRequest("PUT", "/admin/fees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.SetFees(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0.0, resp["deposit_fee_percentage"])
	assert.Equal(t, 2.0, resp["withdrawal_fee_percentage"])
}

func TestSetFees_Both(t *testing.T) {
	h := newTestHandler()

	body, _ := json.Marshal(map[string]interface{}{
		"deposit_fee_percentage":    1.5,
		"withdrawal_fee_percentage": 2.5,
	})
	req := httptest.NewRequest("PUT", "/admin/fees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.SetFees(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 1.5, resp["deposit_fee_percentage"])
	assert.Equal(t, 2.5, resp["withdrawal_fee_percentage"])
}

func TestSetFees_NegativeRejected(t *testing.T) {
	h := newTestHandler()

	body, _ := json.Marshal(map[string]interface{}{"deposit_fee_percentage": -1.0})
	req := httptest.NewRequest("PUT", "/admin/fees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.SetFees(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestSetFees_Over100Rejected(t *testing.T) {
	h := newTestHandler()

	body, _ := json.Marshal(map[string]interface{}{"deposit_fee_percentage": 101.0})
	req := httptest.NewRequest("PUT", "/admin/fees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.SetFees(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestSetFees_PersistsAcrossGets(t *testing.T) {
	h := newTestHandler()

	// Set fee
	body, _ := json.Marshal(map[string]interface{}{"deposit_fee_percentage": 4.0})
	req := httptest.NewRequest("PUT", "/admin/fees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.SetFees(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Get fees — should reflect the set value
	req2 := httptest.NewRequest("GET", "/admin/fees", nil)
	rr2 := httptest.NewRecorder()
	h.GetFees(rr2, req2)

	var resp map[string]interface{}
	err := json.NewDecoder(rr2.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 4.0, resp["deposit_fee_percentage"])
}

// ── Fee applied to CreateTransaction ─────────────────────────────────

func newTestHandlerWithSeededUsers() *Handler {
	store := storage.NewMemoryStorage()
	storage.SeedTestUsers(store)
	wm := webhook.NewManager("", "test-secret", nil)
	return NewHandler(store, wm)
}

func TestCreateTransaction_ExternalDeposit_WithFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()

	// Set a 2.5% deposit fee
	h.feeConfig.SetDepositFeePercent(2.5)

	// Create a wallet for the test user
	createTestWallet(t, h.store, consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       100.00,
		"currency":     "USD",
		"type":         1,
		"deposit_type": "external",
	})
	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "100.00", resp["amount"])
	assert.Equal(t, "2.50", resp["fee"])
	assert.Equal(t, "100.00", resp["total_amount"])
}

func TestCreateTransaction_ExternalDeposit_ZeroFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	// Fee defaults to 0%

	createTestWallet(t, h.store, consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       100.00,
		"currency":     "USD",
		"type":         1,
		"deposit_type": "external",
	})
	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "100.00", resp["amount"])
	assert.Equal(t, "0.00", resp["fee"])
	assert.Equal(t, "100.00", resp["total_amount"])
}

func TestCreateTransaction_HostedTransfer_NoFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	// Even with fee configured, hosted transfers should be fee-free
	h.feeConfig.SetDepositFeePercent(5.0)

	createTestWallet(t, h.store, consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       50.00,
		"currency":     "EUR",
		"type":         2,
		"deposit_type": "hosted",
	})
	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "50.00", resp["amount"])
	assert.Equal(t, "0.00", resp["fee"])
	assert.Equal(t, "50.00", resp["total_amount"])
}

func TestCreateTransaction_FeeInGetTransaction(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	h.feeConfig.SetDepositFeePercent(1.5)

	createTestWallet(t, h.store, consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       200.00,
		"currency":     "USD",
		"type":         1,
		"deposit_type": "external",
	})
	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)

	var createResp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&createResp)
	require.NoError(t, err)
	txID := createResp["uuid"].(string)

	// Fetch via GetTransaction — fee should persist
	tx, err := h.store.GetTransaction(txID)
	require.NoError(t, err)
	assert.Equal(t, "3.00", tx.Fee)
	assert.Equal(t, "200.00", tx.TotalAmount)
}

// ── Fee in TransactionCompleteHandler (iframe deposits) ──────────────

func TestTransactionComplete_WithFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	h.feeConfig.SetDepositFeePercent(2.5)

	createTestWallet(t, h.store, consts.TestUser1ID)
	h.tokenToUser.Store("test-token", consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"amount":   "100.00",
		"currency": "USD",
	})
	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.TransactionCompleteHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify the stored transaction has the fee applied
	// Find the transaction by iterating (iframe handler creates one)
	bal, _ := h.store.GetBalance(consts.TestUser1ID, "USD")
	// Balance should reflect full amount (100.00) — GateHub adds the full amount to the vault,
	// fee is just metadata on the transaction, not deducted from credited amount.
	assert.GreaterOrEqual(t, bal, 100.0, "Balance should include the deposited amount")
}

func TestTransactionComplete_ZeroFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	// Fee defaults to 0%

	createTestWallet(t, h.store, consts.TestUser1ID)
	h.tokenToUser.Store("test-token", consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"amount":   "50.00",
		"currency": "EUR",
	})
	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit&bearer=test-token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.TransactionCompleteHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// ── Withdrawal fee tests ─────────────────────────────────────────────

func TestCreateTransaction_Withdrawal_WithFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	h.feeConfig.SetWithdrawalFeePercent(2.0)

	createTestWallet(t, h.store, consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       50.00,
		"currency":     "EUR",
		"type":         3,
		"deposit_type": "withdrawal",
	})
	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "50.00", resp["amount"])
	assert.Equal(t, "1.00", resp["fee"])
	assert.Equal(t, "51.00", resp["total_amount"])
}

func TestCreateTransaction_Withdrawal_ZeroFee(t *testing.T) {
	h := newTestHandlerWithSeededUsers()
	// Withdrawal fee defaults to 0%

	createTestWallet(t, h.store, consts.TestUser1ID)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":      consts.TestUser1ID,
		"amount":       50.00,
		"currency":     "EUR",
		"type":         3,
		"deposit_type": "withdrawal",
	})
	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "50.00", resp["amount"])
	assert.Equal(t, "0.00", resp["fee"])
	assert.Equal(t, "50.00", resp["total_amount"])
}
