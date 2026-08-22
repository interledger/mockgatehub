package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// balanceSettleTimeout bounds how long we wait for a balance to reach an
// expected value. Transactions created through /core/v1/transactions settle
// asynchronously (the mock defers completion by a couple of seconds when a
// webhook URL is configured), so an assertion made immediately after the POST
// would race the settlement.
const balanceSettleTimeout = 15 * time.Second

// readBalance fetches the user's balance for a currency from the wallet
// balances endpoint. It returns the "available" figure, which is the number
// consumers spend against.
func (tc *TestContext) readBalance(currency string) (float64, error) {
	if tc.walletAddress == "" {
		return 0, fmt.Errorf("no wallet address in test context")
	}

	// Preserve the caller's response: reading a balance is an assertion
	// mechanism, not the behaviour under test, so it must not clobber the
	// response that later steps assert against.
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody
	defer func() {
		tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody
	}()

	if _, err := tc.request("GET", "/core/v1/wallets/{walletAddress}/balances", nil, nil); err != nil {
		return 0, err
	}

	var balances []map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &balances); err != nil {
		return 0, fmt.Errorf("failed to parse balances response: %w. Body: %s", err, string(tc.lastResponseBody))
	}

	for _, balance := range balances {
		vault, ok := balance["vault"].(map[string]interface{})
		if !ok {
			continue
		}
		if assetCode, ok := vault["asset_code"].(string); !ok || assetCode != currency {
			continue
		}
		available, ok := balance["available"].(string)
		if !ok {
			return 0, fmt.Errorf("balance for %s has no string 'available' field", currency)
		}
		value, err := strconv.ParseFloat(available, 64)
		if err != nil {
			return 0, fmt.Errorf("balance for %s is not numeric: %q", currency, available)
		}
		return value, nil
	}

	return 0, fmt.Errorf("currency %s not present in balances response: %s", currency, string(tc.lastResponseBody))
}

// recordBalance snapshots the current balance so a later step can assert on the
// change rather than on an absolute figure, which would depend on seeded data
// and on the configured fee percentages.
func (tc *TestContext) recordBalance(currency string) error {
	value, err := tc.readBalance(currency)
	if err != nil {
		return err
	}
	if tc.recordedBalances == nil {
		tc.recordedBalances = map[string]float64{}
	}
	tc.recordedBalances[currency] = value
	return nil
}

// awaitBalance polls until the balance for currency reaches want, and reports
// what it actually saw if it never does.
func (tc *TestContext) awaitBalance(currency string, want float64) error {
	deadline := time.Now().Add(balanceSettleTimeout)
	var last float64
	var lastErr error

	for time.Now().Before(deadline) {
		last, lastErr = tc.readBalance(currency)
		if lastErr == nil && almostEqual(last, want) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	if lastErr != nil {
		return fmt.Errorf("could not read %s balance while waiting for %.2f: %w", currency, want, lastErr)
	}
	return fmt.Errorf("expected %s balance to settle at %.2f, but it was %.2f after %s",
		currency, want, last, balanceSettleTimeout)
}

func almostEqual(a, b float64) bool {
	diff := a - b
	return diff < 0.005 && diff > -0.005
}

func (tc *TestContext) recordedBalance(currency string) (float64, error) {
	value, ok := tc.recordedBalances[currency]
	if !ok {
		return 0, fmt.Errorf("no recorded %s balance; the scenario must record one first", currency)
	}
	return value, nil
}

// ============ STEPS ============

func (tc *TestContext) iRecordTheBalance(currency string) error {
	return tc.recordBalance(currency)
}

func (tc *TestContext) balanceHasIncreasedBy(currency string, delta float64) error {
	before, err := tc.recordedBalance(currency)
	if err != nil {
		return err
	}
	return tc.awaitBalance(currency, before+delta)
}

func (tc *TestContext) balanceHasDecreasedBy(currency string, delta float64) error {
	before, err := tc.recordedBalance(currency)
	if err != nil {
		return err
	}
	return tc.awaitBalance(currency, before-delta)
}

// balanceIsUnchanged asserts the balance is still what it was. It deliberately
// keeps watching for a while: a bug that credits the balance late would slip
// past a single immediate read.
func (tc *TestContext) balanceIsUnchanged(currency string) error {
	before, err := tc.recordedBalance(currency)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		now, err := tc.readBalance(currency)
		if err != nil {
			return err
		}
		if !almostEqual(now, before) {
			return fmt.Errorf("expected %s balance to stay at %.2f, but it moved to %.2f", currency, before, now)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}

// fundBalance credits the user through an external deposit so that scenarios
// which spend money have something to spend. It waits for settlement.
func (tc *TestContext) fundBalance(currency string, amount float64) error {
	before, err := tc.readBalance(currency)
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"user_id":           tc.userID,
		"receiving_address": tc.walletAddress,
		"amount":            amount,
		"currency":          currency,
		"type":              1,
		"deposit_type":      "external",
	}
	if _, err := tc.request("POST", "/core/v1/transactions", body, map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}); err != nil {
		return err
	}
	if tc.lastResponse.StatusCode != 201 {
		return fmt.Errorf("funding deposit failed with status %d: %s", tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}

	// The credited amount is net of any configured deposit fee, so wait for the
	// balance to rise rather than for a specific figure.
	deadline := time.Now().Add(balanceSettleTimeout)
	for time.Now().Before(deadline) {
		now, err := tc.readBalance(currency)
		if err == nil && now > before {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("funding deposit of %.2f %s never settled (balance stayed at %.2f)", amount, currency, before)
}

func (tc *TestContext) userHasFundedBalance(currency string) error {
	return tc.fundBalance(currency, 1000.00)
}

// postHostedTransferFromUserWallet sends money out of the user's own wallet.
// The mock decides debit-versus-credit from whether sending_address belongs to
// the user, so which address goes where is the behaviour under test.
func (tc *TestContext) postHostedTransferFromUserWallet(amount float64, currency, destination string) error {
	return tc.postHostedTransferBetween(tc.walletAddress, destination, amount, currency)
}

// postHostedTransferToUserWallet brings money in from an address the user does
// not own.
func (tc *TestContext) postHostedTransferToUserWallet(amount float64, currency, source string) error {
	return tc.postHostedTransferBetween(source, tc.walletAddress, amount, currency)
}

func (tc *TestContext) postHostedTransferBetween(sending, receiving string, amount float64, currency string) error {
	body := map[string]interface{}{
		"user_id":           tc.userID,
		"sending_address":   sending,
		"receiving_address": receiving,
		"amount":            amount,
		"currency":          currency,
		"type":              2,
		"deposit_type":      "hosted",
	}
	_, err := tc.request("POST", "/core/v1/transactions", body, map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	})
	return err
}

// responseEchoesSendingAddress checks the transfer response reports which
// wallet the money left from, which is what a consumer reconciles against.
func (tc *TestContext) responseEchoesSendingAddress(expected string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	if expected == "the user wallet" {
		expected = tc.walletAddress
	}
	actual, _ := result["sending_address"].(string)
	if actual != expected {
		return fmt.Errorf("expected sending_address %q, got %q", expected, actual)
	}
	return nil
}
