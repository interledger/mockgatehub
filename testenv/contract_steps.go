package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// contractBodies supplies a minimal valid body for the endpoints that need one,
// so a contract check exercises the route rather than tripping over validation.
func (tc *TestContext) contractBody(method, path string) interface{} {
	switch {
	case strings.HasPrefix(path, "/auth/v1/tokens"):
		return map[string]interface{}{"scope": []string{"deposit"}}
	case path == "/auth/v1/users/managed/email":
		return map[string]interface{}{"email": "contract@example.com", "uuid": tc.userID}
	case strings.HasSuffix(path, "/wallets") && method == http.MethodPost:
		return map[string]interface{}{"name": "contract wallet", "type": 30}
	case path == "/core/v1/transactions":
		return map[string]interface{}{
			"user_id": tc.userID, "amount": 1.00, "currency": "EUR",
			"type": 1, "deposit_type": "external", "receiving_address": tc.walletAddress,
		}
	case strings.HasSuffix(path, "/overrideRiskLevel"):
		return map[string]interface{}{"riskLevel": "low"}
	case strings.HasSuffix(path, "/limits"):
		return []map[string]interface{}{{"type": "dailyOverall", "limit": 1500, "currency": "EUR"}}
	case strings.HasSuffix(path, "/lock"), strings.HasSuffix(path, "/unlock"):
		return map[string]interface{}{"note": "contract check"}
	case strings.Contains(path, "/token/card-data"), strings.Contains(path, "/token/pin"):
		body := map[string]interface{}{"cardId": tc.cardID}
		if tc.keys != nil {
			body["publicKey"] = tc.keys.publicKeyB64
		}
		return body
	case strings.HasSuffix(path, "/plastic"):
		return map[string]interface{}{}
	case strings.HasPrefix(path, "/id/v1/hubs/"):
		return map[string]interface{}{"riskLevel": "low"}
	default:
		if method == http.MethodPost || method == http.MethodPut {
			return map[string]interface{}{}
		}
		return nil
	}
}

// endpointIsServed asserts a route exists and is not rejecting the request out
// of hand. It deliberately does not pin the exact status: the contract being
// guarded is that the path and method are routed and authenticated, so a
// renamed route, a changed prefix or a narrowed method fails here.
func (tc *TestContext) endpointIsServed(method, path string) error {
	headers := map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
		"x-gatehub-card-app-id":       "test-app-id",
	}

	if _, err := tc.request(method, path, tc.contractBody(method, path), headers); err != nil {
		return err
	}

	status := tc.lastResponse.StatusCode
	switch status {
	case http.StatusNotFound:
		return fmt.Errorf("%s %s is not routed (404). A consumer calling it would fail", method, tc.replacePlaceholders(path))
	case http.StatusMethodNotAllowed:
		return fmt.Errorf("%s %s is routed but not for this method (405)", method, tc.replacePlaceholders(path))
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s %s rejected valid credentials (%d): %s",
			method, tc.replacePlaceholders(path), status, string(tc.lastResponseBody))
	}

	if status >= 500 {
		return fmt.Errorf("%s %s failed with %d: %s",
			method, tc.replacePlaceholders(path), status, string(tc.lastResponseBody))
	}
	return nil
}

// ---- response shape checks ----

// cardObjectCarriesConsumerFields guards the card fields consumers read. The
// expiry date in particular was dropped by a downstream fork.
func (tc *TestContext) cardObjectCarriesConsumerFields() error {
	var card map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &card); err != nil {
		return fmt.Errorf("could not parse card: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	return requireFields("card", card,
		"id", "accountId", "customerId", "nameOnCard", "maskedPan", "status", "expiryDate", "productCode")
}

// cardTransactionsCarryConsumerFields guards the card transaction fields
// consumers read, including the integer id and cardId they key off.
func (tc *TestContext) cardTransactionsCarryConsumerFields() error {
	var listing struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &listing); err != nil {
		return fmt.Errorf("could not parse listing: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	if len(listing.Data) == 0 {
		return fmt.Errorf("no transactions listed, so this check would be vacuous")
	}

	for _, tx := range listing.Data {
		if err := requireFields("card transaction", tx,
			"id", "transactionId", "cardId", "type", "txStatus",
			"transactionAmount", "transactionCurrency", "billingAmount", "billingCurrency",
			"createdAt", "vaultId"); err != nil {
			return err
		}
		// The integer id and cardId are numbers to consumers, not strings.
		for _, field := range []string{"id", "cardId"} {
			if _, ok := tx[field].(float64); !ok {
				return fmt.Errorf("card transaction %q must be a number, got %T", field, tx[field])
			}
		}
	}
	return nil
}

// userStateCarriesConsumerFields guards the user state shape consumers read.
func (tc *TestContext) userStateCarriesConsumerFields() error {
	var state struct {
		Profile       map[string]interface{}   `json:"profile"`
		Verifications []map[string]interface{} `json:"verifications"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &state); err != nil {
		return fmt.Errorf("could not parse user state: %w. Body: %s", err, string(tc.lastResponseBody))
	}

	if err := requireFields("profile", state.Profile,
		"first_name", "last_name", "address_country_code", "address_city",
		"address_street1", "address_street2"); err != nil {
		return err
	}

	if len(state.Verifications) == 0 {
		return fmt.Errorf("user state carries no verifications")
	}
	return requireFields("verification", state.Verifications[0], "status", "state", "provider_type")
}

func requireFields(what string, obj map[string]interface{}, fields ...string) error {
	if obj == nil {
		return fmt.Errorf("%s is absent", what)
	}
	missing := make([]string, 0)
	for _, field := range fields {
		if _, ok := obj[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s is missing the field(s) consumers read: %s", what, strings.Join(missing, ", "))
	}
	return nil
}
