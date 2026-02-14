package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ============ FEE CONFIGURATION STEPS ============

// depositFeeConfigured sets the deposit fee percentage via PUT /admin/fees
func (tc *TestContext) depositFeeConfigured(percent float64) error {
	body := map[string]interface{}{
		"deposit_fee_percentage": percent,
	}
	// Store current HMAC state and clear it (admin endpoints require no auth)
	savedSecret := tc.appSecret
	tc.appSecret = ""
	_, err := tc.requestRaw("PUT", "/admin/fees", mustJSON(body), "application/json", nil)
	tc.appSecret = savedSecret
	if err != nil {
		return err
	}
	if tc.lastResponse.StatusCode != 200 {
		return fmt.Errorf("failed to set deposit fee: status %d, body: %s", tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}
	return nil
}

// withdrawalFeeConfigured sets the withdrawal fee percentage via PUT /admin/fees
func (tc *TestContext) withdrawalFeeConfigured(percent float64) error {
	body := map[string]interface{}{
		"withdrawal_fee_percentage": percent,
	}
	savedSecret := tc.appSecret
	tc.appSecret = ""
	_, err := tc.requestRaw("PUT", "/admin/fees", mustJSON(body), "application/json", nil)
	tc.appSecret = savedSecret
	if err != nil {
		return err
	}
	if tc.lastResponse.StatusCode != 200 {
		return fmt.Errorf("failed to set withdrawal fee: status %d, body: %s", tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}
	return nil
}

// getAdminFeesWithoutAuth does a GET /admin/fees without HMAC headers
func (tc *TestContext) getAdminFeesWithoutAuth() error {
	savedSecret := tc.appSecret
	tc.appSecret = ""
	_, err := tc.requestRaw("GET", "/admin/fees", "", "", nil)
	tc.appSecret = savedSecret
	return err
}

// putAdminFeesWithoutAuth does a PUT /admin/fees without HMAC headers
func (tc *TestContext) putAdminFeesWithoutAuth(bodyJSON string) error {
	savedSecret := tc.appSecret
	tc.appSecret = ""
	_, err := tc.requestRaw("PUT", "/admin/fees", bodyJSON, "application/json", nil)
	tc.appSecret = savedSecret
	return err
}

// putAdminFeesWithoutAnyHMAC does a PUT /admin/fees entirely without HMAC headers
func (tc *TestContext) putAdminFeesWithoutAnyHMAC(bodyJSON string) error {
	_, err := tc.requestRaw("PUT", "/admin/fees", bodyJSON, "application/json", nil)
	return err
}

// depositFeePercentageIs checks that the deposit_fee_percentage in the response matches
func (tc *TestContext) depositFeePercentageIs(expected float64) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	actual, ok := result["deposit_fee_percentage"].(float64)
	if !ok {
		return fmt.Errorf("deposit_fee_percentage not found or not a number in response: %s", string(tc.lastResponseBody))
	}
	if actual != expected {
		return fmt.Errorf("expected deposit_fee_percentage %.2f, got %.2f", expected, actual)
	}
	return nil
}

// withdrawalFeePercentageIs checks that the withdrawal_fee_percentage in the response matches
func (tc *TestContext) withdrawalFeePercentageIs(expected float64) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	actual, ok := result["withdrawal_fee_percentage"].(float64)
	if !ok {
		return fmt.Errorf("withdrawal_fee_percentage not found or not a number in response: %s", string(tc.lastResponseBody))
	}
	if actual != expected {
		return fmt.Errorf("expected withdrawal_fee_percentage %.2f, got %.2f", expected, actual)
	}
	return nil
}

// transactionFeeIs checks that the "fee" field in the response matches the expected value
func (tc *TestContext) transactionFeeIs(expected string) error {
	return tc.checkFieldValue("fee", expected)
}

// transactionTotalAmountIs checks that the "total_amount" field in the response matches
func (tc *TestContext) transactionTotalAmountIs(expected string) error {
	return tc.checkFieldValue("total_amount", expected)
}

// transactionAmountIs checks that the "amount" field in the response matches
func (tc *TestContext) transactionAmountIs(expected string) error {
	return tc.checkFieldValue("amount", expected)
}

// postDepositWithFeeFields creates a deposit transaction for fee testing
func (tc *TestContext) postDepositWithFeeFields(txType int, depositType string, amount float64, currency string) error {
	body := map[string]interface{}{
		"user_id":      tc.userID,
		"type":         txType,
		"deposit_type": depositType,
		"amount":       amount,
		"currency":     currency,
	}
	_, err := tc.request("POST", "/core/v1/transactions", body, nil)
	if err != nil {
		return err
	}

	// Store the transaction ID for later retrieval
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	if txID, ok := result["uuid"].(string); ok {
		tc.transactionID = txID
	}
	return nil
}

// getTransactionByID retrieves a transaction by its stored ID
func (tc *TestContext) getTransactionByID() error {
	if tc.transactionID == "" {
		return fmt.Errorf("no transaction ID available")
	}
	path := fmt.Sprintf("/core/v1/transactions/%s", tc.transactionID)
	_, err := tc.request("GET", path, nil, nil)
	return err
}

// createWithdrawalTransaction creates a withdrawal transaction for the given amount and currency
func (tc *TestContext) createWithdrawalTransaction(amount float64, currency string) error {
	body := map[string]interface{}{
		"user_id":      tc.userID,
		"type":         3, // Withdrawal type
		"deposit_type": "withdrawal",
		"amount":       amount,
		"currency":     currency,
	}
	_, err := tc.request("POST", "/core/v1/transactions", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	if txID, ok := result["uuid"].(string); ok {
		tc.transactionID = txID
	}
	return nil
}

// postHostedTransferWithFeeFields creates a hosted (type 2) transaction
func (tc *TestContext) postHostedTransferWithFeeFields(amount float64, currency string, txType int, depositType string) error {
	body := map[string]interface{}{
		"user_id":      tc.userID,
		"amount":       amount,
		"currency":     currency,
		"type":         txType,
		"deposit_type": depositType,
	}
	_, err := tc.request("POST", "/core/v1/transactions", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	if txID, ok := result["uuid"].(string); ok {
		tc.transactionID = txID
	}
	return nil
}

// responseStatusWithStatusText checks both status code and a "status" field in the body
func (tc *TestContext) responseStatusWithStatusText(code int, status string) error {
	if tc.lastResponse.StatusCode != code {
		return fmt.Errorf("expected status %d, got %d. Body: %s", code, tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	if s, ok := result["status"].(string); !ok || s != status {
		return fmt.Errorf("expected status field %q, got %v", status, result["status"])
	}
	return nil
}

// mustJSON marshals a value to a JSON string, panicking on error
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// parseAmountCurrency parses "50.00 EUR" into amount float64 and currency string
func parseAmountCurrency(s string) (float64, string, error) {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("expected 'amount currency', got %q", s)
	}
	amount, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid amount %q: %w", parts[0], err)
	}
	return amount, parts[1], nil
}
