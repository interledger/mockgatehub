package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============ WALLET STEPS ============

func (tc *TestContext) walletsArrayReturned() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if wallets, ok := result["wallets"].([]interface{}); !ok || len(wallets) == 0 {
		return fmt.Errorf("expected wallets array with at least one wallet, got %v", result)
	}

	return nil
}

func (tc *TestContext) firstWalletStartsWith(prefix string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	wallets, ok := result["wallets"].([]interface{})
	if !ok || len(wallets) == 0 {
		return fmt.Errorf("no wallets found")
	}

	wallet, ok := wallets[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("wallet is not an object")
	}

	address, ok := wallet["address"].(string)
	if !ok {
		return fmt.Errorf("no address in wallet")
	}

	if !strings.HasPrefix(address, prefix) {
		return fmt.Errorf("expected address to start with %s, got %s", prefix, address)
	}

	tc.walletAddress = address
	return nil
}

func (tc *TestContext) managedUserWithWalletAddress() error {
	// Create a user and wallet
	if err := tc.existingManagedUserGeneric(); err != nil {
		return err
	}

	// Create a wallet using the correct endpoint with userID
	resp, err := tc.request("POST", "/core/v1/users/{userId}/wallets", nil, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return fmt.Errorf("failed to unmarshal wallet response: %w. Response: %s. Status: %d", err, string(tc.lastResponseBody), resp.StatusCode)
	}

	// Extract wallet address
	if address, ok := result["address"].(string); ok {
		tc.walletAddress = address
	}

	tc.lastResponse = resp
	return nil
}
