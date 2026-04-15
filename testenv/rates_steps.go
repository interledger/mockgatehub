package main

import (
	"encoding/json"
	"fmt"
	"math"
)

// ============ RATES STEPS ============

func (tc *TestContext) mockgatehubRunningWithHeaders() error {
	tc.appID = "local-test-app-id"
	tc.appSecret = "local-test-app-secret"
	return nil
}

func (tc *TestContext) payloadHasCounterCurrency() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	_, ok1 := result["counter_currency"]
	_, ok2 := result["counterCurrency"]
	_, ok3 := result["counter"]
	if !ok1 && !ok2 && !ok3 {
		return fmt.Errorf("missing counter_currency, counterCurrency, or counter in: %s", string(tc.lastResponseBody))
	}

	return nil
}

func (tc *TestContext) rateEntryExists() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	// Count non-counter fields
	count := 0
	for key := range result {
		if key != "counter_currency" && key != "counterCurrency" {
			count++
		}
	}

	if count == 0 {
		return fmt.Errorf("no rate entries")
	}

	return nil
}

func (tc *TestContext) responseHasVaults() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	vaults, ok := result["vaults"].([]interface{})
	if !ok || len(vaults) == 0 {
		return fmt.Errorf("no vaults array or empty")
	}

	return nil
}

// extractRate parses the rate value for a given currency from the rates response
func (tc *TestContext) extractRate(currency string) (float64, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return 0, err
	}

	entry, ok := result[currency]
	if !ok {
		return 0, fmt.Errorf("currency %s not found in response", currency)
	}

	rateObj, ok := entry.(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("unexpected rate entry format for %s", currency)
	}

	rate, ok := rateObj["rate"].(float64)
	if !ok {
		return 0, fmt.Errorf("rate for %s is not a number", currency)
	}

	return rate, nil
}

func (tc *TestContext) counterFieldIs(expected string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	counter, ok := result["counter"].(string)
	if !ok {
		return fmt.Errorf("counter field missing or not a string")
	}
	if counter != expected {
		return fmt.Errorf("expected counter %q, got %q", expected, counter)
	}
	return nil
}

func (tc *TestContext) rateForCurrencyIs(currency string, expected float64) error {
	rate, err := tc.extractRate(currency)
	if err != nil {
		return err
	}
	if math.Abs(rate-expected) > 0.0001 {
		return fmt.Errorf("expected %s rate %f, got %f", currency, expected, rate)
	}
	return nil
}

func (tc *TestContext) rateForCurrencyApprox(currency string, expected, tolerance float64) error {
	rate, err := tc.extractRate(currency)
	if err != nil {
		return err
	}
	if math.Abs(rate-expected) > tolerance {
		return fmt.Errorf("expected %s rate ~%f (±%f), got %f", currency, expected, tolerance, rate)
	}
	return nil
}

func (tc *TestContext) saveRateAs(currency, name string) error {
	rate, err := tc.extractRate(currency)
	if err != nil {
		return err
	}
	tc.savedRates[name] = rate
	return nil
}

func (tc *TestContext) rateIsInverseOfSaved(currency, savedName string, tolerance float64) error {
	rate, err := tc.extractRate(currency)
	if err != nil {
		return err
	}
	saved, ok := tc.savedRates[savedName]
	if !ok {
		return fmt.Errorf("no saved rate named %q", savedName)
	}
	expected := 1.0 / saved
	if math.Abs(rate-expected) > tolerance {
		return fmt.Errorf("expected %s rate ~%f (1/%f, ±%f), got %f", currency, expected, saved, tolerance, rate)
	}
	return nil
}
