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
	// Create a managed user
	if err := tc.existingManagedUserGeneric(); err != nil {
		return err
	}

	// GET the user — GateHub auto-provisions a wallet for managed users,
	// so we just retrieve it rather than explicitly creating one.
	resp, err := tc.request("GET", "/core/v1/users/{userId}", nil, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return fmt.Errorf("failed to unmarshal user wallets response: %w. Response: %s. Status: %d", err, string(tc.lastResponseBody), resp.StatusCode)
	}

	// Extract wallet address from the first wallet in the array
	if wallets, ok := result["wallets"].([]interface{}); ok && len(wallets) > 0 {
		if wallet, ok := wallets[0].(map[string]interface{}); ok {
			if address, ok := wallet["address"].(string); ok {
				tc.walletAddress = address
			}
		}
	}

	if tc.walletAddress == "" {
		return fmt.Errorf("no wallet address found in user response: %s", string(tc.lastResponseBody))
	}

	tc.lastResponse = resp
	return nil
}
