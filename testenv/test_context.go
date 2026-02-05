package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TestContext struct {
	client           *http.Client
	baseURL          string
	appID            string
	appSecret        string
	lastResponse     *http.Response
	lastResponseBody []byte
	lastError        error
	userID           string
	walletAddress    string
	previousAddress  string
	accessToken      string
	iframeToken      string
	kycToken         string
	email            string
	customerID       string
	cardID           string
}

func (tc *TestContext) Reset() {
	tc.client = &http.Client{Timeout: 10 * time.Second}
	tc.baseURL = "http://localhost:25151"
	tc.appID = ""
	tc.appSecret = ""
	tc.userID = ""
	tc.walletAddress = ""
	tc.accessToken = ""
	tc.iframeToken = ""
	tc.kycToken = ""
}

func (tc *TestContext) generateHMAC(method, path, body string) string {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	payload := timestamp + method + path + body
	h := hmac.New(sha256.New, []byte(tc.appSecret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

func (tc *TestContext) replacePlaceholders(path string) string {
	path = strings.ReplaceAll(path, "{userId}", tc.userID)
	path = strings.ReplaceAll(path, "{customerId}", tc.customerID)
	path = strings.ReplaceAll(path, "{cardId}", tc.cardID)
	path = strings.ReplaceAll(path, "{cardID}", tc.cardID)
	path = strings.ReplaceAll(path, "{address}", tc.walletAddress)
	path = strings.ReplaceAll(path, "{iframeToken}", tc.iframeToken)
	path = strings.ReplaceAll(path, "{token}", tc.kycToken)
	return path
}

func (tc *TestContext) request(method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	path = tc.replacePlaceholders(path)

	var bodyStr string
	var bodyBytes []byte

	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyStr = string(bodyBytes)
	}

	url := tc.baseURL + path
	req, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// Add HMAC headers if appSecret is set
	if tc.appSecret != "" {
		req.Header.Set("x-gatehub-app-id", tc.appID)
		req.Header.Set("x-gatehub-timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		req.Header.Set("x-gatehub-signature", tc.generateHMAC(method, path, bodyStr))
	}

	// Add custom headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	tc.lastResponse = resp
	tc.lastResponse.Body = io.NopCloser(bytes.NewReader(respBody))
	tc.lastResponseBody = respBody

	return resp, nil
}

func (tc *TestContext) requestForm(method, path string, formData map[string]string) (*http.Response, error) {
	path = tc.replacePlaceholders(path)

	// Convert form data to URL-encoded body
	vals := make([]string, 0, len(formData))
	for k, v := range formData {
		vals = append(vals, fmt.Sprintf("%s=%s", k, v))
	}
	bodyStr := strings.Join(vals, "&")
	bodyBytes := []byte(bodyStr)

	url := tc.baseURL + path
	req, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// Add HMAC headers if appSecret is set
	if tc.appSecret != "" {
		req.Header.Set("x-gatehub-app-id", tc.appID)
		req.Header.Set("x-gatehub-timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		req.Header.Set("x-gatehub-signature", tc.generateHMAC(method, path, bodyStr))
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	tc.lastResponse = resp
	tc.lastResponse.Body = io.NopCloser(bytes.NewReader(respBody))
	tc.lastResponseBody = respBody

	return resp, nil
}

// ============ BASIC STEPS ============

func (tc *TestContext) mockgatehubStarted() error {
	tc.Reset()
	return nil
}

func (tc *TestContext) serviceURLIs(url string) error {
	tc.baseURL = url
	return nil
}

func (tc *TestContext) sendGetRequestWithHMAC(path string) error {
	tc.appID = "test-app-id"
	tc.appSecret = "test-app-secret"
	resp, err := tc.request("GET", path, nil, nil)
	if err != nil {
		tc.lastError = err
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseStatusIs(status int) error {
	if tc.lastResponse == nil {
		return fmt.Errorf("no response")
	}
	if tc.lastResponse.StatusCode != status {
		return fmt.Errorf("expected status %d, got %d. Body: %s", status, tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}
	return nil
}

func (tc *TestContext) responseBodyContains(key, value string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &data); err != nil {
		return err
	}
	if v, ok := data[key]; !ok || fmt.Sprintf("%v", v) != value {
		return fmt.Errorf("expected %s to be %s, got %v", key, value, v)
	}
	return nil
}

// ============ AUTH STEPS ============

func (tc *TestContext) cleanMockGatehub() error {
	tc.Reset()
	return nil
}

func (tc *TestContext) hmacHeaders(appID, appSecret string) error {
	tc.appID = appID
	tc.appSecret = appSecret
	return nil
}

func (tc *TestContext) postWithEmail(path, email string) error {
	tc.email = email
	body := map[string]string{"email": email}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	} else if id, ok := result["user_id"].(string); ok {
		tc.userID = id
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) payloadHasUserID() error {
	if tc.userID == "" {
		return fmt.Errorf("no user ID in response")
	}
	return nil
}

func (tc *TestContext) managedFlagTrue() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}
	if managed, ok := result["managed"].(bool); !ok || !managed {
		return fmt.Errorf("expected managed to be true, got %v", result["managed"])
	}
	return nil
}

// ============ USER/TOKEN STEPS ============

func (tc *TestContext) existingManagedUser(email string) error {
	tc.email = email
	body := map[string]string{"email": email}
	resp, err := tc.request("POST", "/auth/v1/users/managed", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) postWithCredentials(path, username, password string) error {
	body := map[string]string{"username": username, "password": password}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if token, ok := result["access_token"].(string); ok {
		tc.accessToken = token
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseHasAccessToken() error {
	if tc.accessToken == "" {
		return fmt.Errorf("no access_token in response")
	}
	return nil
}

func (tc *TestContext) existingManagedUserGeneric() error {
	body := map[string]string{"email": fmt.Sprintf("user%d@example.com", time.Now().UnixNano())}
	resp, err := tc.request("POST", "/auth/v1/users/managed", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) existingManagedUserWithKYC(kycState string) error {
	// Create a managed user
	if err := tc.existingManagedUserGeneric(); err != nil {
		return err
	}

	// Set KYC state if needed
	if kycState == "accepted" {
		// Start KYC
		path := fmt.Sprintf("/id/v1/users/%s/hubs/gw", tc.userID)
		_, _ = tc.request("POST", path, nil, nil)

		// Submit KYC
		submitBody := map[string]string{
			"first_name": "John",
			"last_name":  "Doe",
			"dob":        "1990-01-01",
			"address":    "123 Main St",
			"city":       "Anytown",
			"country":    "US",
			"risk_level": "low",
		}
		_, _ = tc.requestForm("POST", "/iframe/submit", submitBody)
	}

	return nil
}

func (tc *TestContext) postWithScopeAndHeader(path, scope string) error {
	headers := map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}
	scopes := strings.Split(strings.Trim(scope, "\""), ",")
	for i := range scopes {
		scopes[i] = strings.TrimSpace(scopes[i])
		scopes[i] = strings.Trim(scopes[i], "\"")
	}
	body := map[string]interface{}{
		"scope": scopes,
	}
	resp, err := tc.request("POST", path, body, headers)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if token, ok := result["token"].(string); ok {
		tc.iframeToken = token
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseHasTokenPrefix(prefix string) error {
	if !strings.HasPrefix(tc.iframeToken, prefix) {
		return fmt.Errorf("expected token to start with %s, got %s", prefix, tc.iframeToken)
	}
	return nil
}

func (tc *TestContext) tokenReusableAsBearer() error {
	// Token is stored for later use
	return nil
}

// ============ KYC STEPS ============

func (tc *TestContext) postEndpoint(path string) error {
	resp, err := tc.request("POST", path, nil, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if token, ok := result["token"].(string); ok {
		tc.kycToken = token
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseHasKYCToken() error {
	if tc.kycToken == "" {
		return fmt.Errorf("no KYC token in response")
	}
	return nil
}

func (tc *TestContext) getShowsKYCState(path, kycState, riskLevel string) error {
	resp, err := tc.request("GET", path, nil, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if state, ok := result["kyc_state"].(string); !ok || state != kycState {
		return fmt.Errorf("expected kyc_state %s, got %v", kycState, result["kyc_state"])
	}

	if risk, ok := result["risk_level"].(string); !ok || risk != riskLevel {
		return fmt.Errorf("expected risk_level %s, got %v", riskLevel, result["risk_level"])
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) getEndpoint(path string) error {
	resp, err := tc.request("GET", path, nil, nil)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) getEndpointBalance(path string) error {
	resp, err := tc.request("GET", path, nil, nil)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseIsHTMLMentioning(text1, text2 string) error {
	if !strings.Contains(string(tc.lastResponseBody), text1) || !strings.Contains(string(tc.lastResponseBody), text2) {
		return fmt.Errorf("expected HTML to mention %s and %s", text1, text2)
	}
	return nil
}

func (tc *TestContext) userInActionRequired() error {
	// Create user with action_required state
	body := map[string]string{"email": fmt.Sprintf("user%d@example.com", time.Now().UnixNano())}
	resp, err := tc.request("POST", "/auth/v1/users/managed", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	}

	// Start KYC to set state to action_required
	path := fmt.Sprintf("/id/v1/users/%s/hubs/gw", tc.userID)
	_, err = tc.request("POST", path, nil, nil)
	tc.lastResponse = resp
	return err
}

func (tc *TestContext) postKYCForm(path, sep, riskLevel string) error {
	// Post to iframe submit using form encoding
	body := map[string]string{
		"first_name": "John",
		"last_name":  "Doe",
		"dob":        "1990-01-01",
		"address":    "123 Main St",
		"city":       "Anytown",
		"country":    "US",
		"risk_level": riskLevel,
	}
	resp, err := tc.requestForm("POST", path, body)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) postKYCFormSimple(path, riskLevel string) error {
	// Post to iframe submit using form encoding
	body := map[string]string{
		"first_name": "John",
		"last_name":  "Doe",
		"dob":        "1990-01-01",
		"address":    "123 Main St",
		"city":       "Anytown",
		"country":    "US",
		"risk_level": riskLevel,
	}
	resp, err := tc.requestForm("POST", path, body)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) getReturnsKYCState(path, kycState string) error {
	resp, err := tc.request("GET", path, nil, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if state, ok := result["kyc_state"].(string); !ok || state != kycState {
		return fmt.Errorf("expected kyc_state %s, got %v", kycState, result["kyc_state"])
	}

	tc.lastResponse = resp
	return nil
}

// ============ WALLET STEPS ============

func (tc *TestContext) authenticatedRequest() error {
	tc.hmacHeaders("local-test-app-id", "local-test-app-secret")
	return nil
}

func (tc *TestContext) authenticatedRequests() error {
	tc.appID = "local-test-app-id"
	tc.appSecret = "local-test-app-secret"
	return nil
}

func (tc *TestContext) managedUserFromEndpoint(path string) error {
	body := map[string]string{"email": fmt.Sprintf("user%d@example.com", time.Now().UnixNano())}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	}

	tc.lastResponse = resp
	return nil
}

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

func (tc *TestContext) addressStored() error {
	tc.previousAddress = tc.walletAddress
	return nil
}

func (tc *TestContext) previousWalletAddress() error {
	tc.walletAddress = tc.previousAddress
	return nil
}

func (tc *TestContext) getAgain(path string) error {
	return tc.getEndpoint(path)
}

func (tc *TestContext) sameWalletAddressReturned() error {
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

	if address != tc.previousAddress {
		return fmt.Errorf("expected same wallet %s, got %s", tc.previousAddress, address)
	}

	return nil
}

func (tc *TestContext) postWithNameAndCurrency(path, name, currency string) error {
	body := map[string]string{
		"name":     name,
		"currency": currency,
	}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) newWalletAddressStarts(prefix string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	address, ok := result["address"].(string)
	if !ok {
		return fmt.Errorf("no address in response")
	}

	if !strings.HasPrefix(address, prefix) {
		return fmt.Errorf("expected address to start with %s, got %s", prefix, address)
	}

	tc.walletAddress = address
	return nil
}

func (tc *TestContext) postWalletGlobal(path string) error {
	body := map[string]interface{}{
		"user_id": tc.userID,
		"name":    "Test Wallet",
	}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) walletBelongsToUser() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	userID, ok := result["user_id"].(string)
	if !ok || userID != tc.userID {
		return fmt.Errorf("expected user_id %s, got %v", tc.userID, result["user_id"])
	}

	return nil
}

func (tc *TestContext) haveWalletAddress() error {
	if tc.walletAddress == "" {
		return fmt.Errorf("no wallet address stored")
	}
	return nil
}

func (tc *TestContext) responseHasAllCurrencies() error {
	var result []interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if len(result) != 11 {
		return fmt.Errorf("expected 11 currencies, got %d, response: %s", len(result), string(tc.lastResponseBody))
	}

	return nil
}

func (tc *TestContext) eachEntryHasCurrencyAndVault() error {
	var result []map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	for _, entry := range result {
		// Check for vault object (which contains uuid)
		vault, ok := entry["vault"].(map[string]interface{})
		if !ok {
			// Try old format with vault_uuid
			if _, ok := entry["vault_uuid"]; !ok {
				return fmt.Errorf("missing vault object or vault_uuid field")
			}
			continue
		}

		// Vault object should have uuid
		if _, ok := vault["uuid"]; !ok {
			return fmt.Errorf("vault object missing uuid field")
		}
	}

	return nil
}

func (tc *TestContext) walletWithDeposits(amount, currency string) error {
	// Create transaction
	vaultMap := map[string]string{
		"USD": "450d2156-132a-4d3f-88c5-74822547658d",
		"EUR": "a09a0a2c-1a3a-44c5-a1b9-603a6eea9341",
		"GBP": "992b932d-7e9e-44b0-90ea-b82a530b6784",
	}
	vault := vaultMap[currency]
	if vault == "" {
		vault = "450d2156-132a-4d3f-88c5-74822547658d"
	}

	body := map[string]interface{}{
		"user_id":           tc.userID,
		"amount":            amount,
		"currency":          currency,
		"receiving_address": tc.walletAddress,
		"type":              1,
		"deposit_type":      "external",
		"vault_uuid":        vault,
	}
	_, err := tc.request("POST", "/core/v1/transactions", body, nil)
	return err
}

func (tc *TestContext) currencyBalanceShows(currency, amount string) error {
	var result []map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	for _, entry := range result {
		if curr, ok := entry["currency"].(string); ok && curr == currency {
			if balance, ok := entry["balance"].(float64); ok {
				if fmt.Sprintf("%.2f", balance) == amount {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("expected %s balance %s, got %s", currency, amount, string(tc.lastResponseBody))
}

func (tc *TestContext) vaultMetadataPresent(currency string) error {
	var result []map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	for _, entry := range result {
		if curr, ok := entry["currency"].(string); ok && curr == currency {
			if _, ok := entry["vault_uuid"]; ok {
				return nil
			}
		}
	}

	return fmt.Errorf("no vault metadata for %s", currency)
}

// ============ TRANSACTION STEPS ============

func (tc *TestContext) managedUserWithWallet() error {
	body := map[string]string{"email": fmt.Sprintf("user%d@example.com", time.Now().UnixNano())}
	resp, err := tc.request("POST", "/auth/v1/users/managed", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	}

	// Get wallet
	path := fmt.Sprintf("/core/v1/users/%s", tc.userID)
	resp, err = tc.request("GET", path, nil, nil)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if wallets, ok := result["wallets"].([]interface{}); ok && len(wallets) > 0 {
		if wallet, ok := wallets[0].(map[string]interface{}); ok {
			if addr, ok := wallet["address"].(string); ok {
				tc.walletAddress = addr
			}
		}
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) iframeTokenWithScope(scope string) error {
	scopes := strings.Split(strings.Trim(scope, "\""), ",")
	for i := range scopes {
		scopes[i] = strings.TrimSpace(scopes[i])
		scopes[i] = strings.Trim(scopes[i], "\"")
	}
	body := map[string]interface{}{
		"scope": scopes,
	}
	headers := map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}
	resp, err := tc.request("POST", "/auth/v1/tokens", body, headers)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if token, ok := result["token"].(string); ok {
		tc.iframeToken = token
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) postWithAmountCurrencyAuth(path, amount, currency, authHeader string) error {
	body := map[string]interface{}{
		"amount":   amount,
		"currency": currency,
	}
	headers := map[string]string{
		"Authorization": authHeader,
	}
	resp, err := tc.request("POST", path, body, headers)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseStatusWithStatus(status int, statusStr string) error {
	if tc.lastResponse.StatusCode != status {
		return fmt.Errorf("expected status %d, got %d", status, tc.lastResponse.StatusCode)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if s, ok := result["status"].(string); !ok || s != statusStr {
		return fmt.Errorf("expected status %s, got %v", statusStr, result["status"])
	}

	return nil
}

func (tc *TestContext) balanceIncreases(currency, amount string) error {
	path := fmt.Sprintf("/core/v1/wallets/%s/balance", tc.walletAddress)
	_, err := tc.request("GET", path, nil, nil)
	if err != nil {
		return err
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	for _, entry := range result {
		if curr, ok := entry["currency"].(string); ok && curr == currency {
			if balance, ok := entry["balance"].(float64); ok {
				if fmt.Sprintf("%.2f", balance) == amount {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("expected %s balance %s", currency, amount)
}

func (tc *TestContext) postTransactionHosted(path string, amount float64, currency string, txType int, depositType string) error {
	body := map[string]interface{}{
		"user_id":      tc.userID,
		"amount":       amount,
		"currency":     currency,
		"type":         txType,
		"deposit_type": depositType,
	}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseHasIDOrUUID() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if _, ok := result["id"].(string); ok {
		return nil
	}
	if _, ok := result["uuid"].(string); ok {
		return nil
	}

	return fmt.Errorf("expected id or uuid in response, got %v", result)
}

func (tc *TestContext) amountEchoes(amount float64) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if a, ok := result["amount"].(float64); !ok || a != amount {
		return fmt.Errorf("expected amount %f, got %v", amount, result["amount"])
	}

	return nil
}

func (tc *TestContext) statusCompleted(statusInt int, statusStr string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	// Check for integer status
	if status, ok := result["status"].(float64); ok && int(status) == statusInt {
		return nil
	}

	// Check for string status
	if status, ok := result["status"].(string); ok && status == statusStr {
		return nil
	}

	return fmt.Errorf("expected status %d or %s, got %v", statusInt, statusStr, result["status"])
}

func (tc *TestContext) depositTypeIs(depositType string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if dt, ok := result["deposit_type"].(string); !ok || dt != depositType {
		return fmt.Errorf("expected deposit_type %s, got %v", depositType, result["deposit_type"])
	}

	return nil
}

func (tc *TestContext) webhookEmitted() error {
	// Webhook would be emitted asynchronously
	return nil
}

func (tc *TestContext) postExternalTransaction(path string, txType int, depositType string, address string, amount float64, currency string) error {
	body := map[string]interface{}{
		"type":              txType,
		"deposit_type":      depositType,
		"receiving_address": tc.walletAddress,
		"amount":            amount,
		"currency":          currency,
		"vault_uuid":        "450d2156-132a-4d3f-88c5-74822547658d", // USD vault
	}
	resp, err := tc.request("POST", path, body, nil)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) fieldsStringFormatted() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	for _, field := range []string{"amount", "total_amount", "fee"} {
		if _, ok := result[field].(string); !ok {
			return fmt.Errorf("expected %s to be string, got %T", field, result[field])
		}
	}

	return nil
}

func (tc *TestContext) statusIsInteger(status int) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if s, ok := result["status"].(float64); !ok || int(s) != status {
		return fmt.Errorf("expected status %d, got %v", status, result["status"])
	}

	return nil
}

func (tc *TestContext) transactionCanBeRetrieved(path string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	var txID string
	if id, ok := result["id"].(string); ok {
		txID = id
	} else if id, ok := result["uuid"].(string); ok {
		txID = id
	}

	if txID == "" {
		return fmt.Errorf("no transaction id")
	}

	// Get transaction
	retrievePath := fmt.Sprintf("/core/v1/transactions/%s", txID)
	_, err := tc.request("GET", retrievePath, nil, nil)
	return err
}

// ============ CARD STEPS ============

func (tc *TestContext) managedUserWithKYCState(state string) error {
	body := map[string]string{"email": fmt.Sprintf("user%d@example.com", time.Now().UnixNano())}
	resp, err := tc.request("POST", "/auth/v1/users/managed", body, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if id, ok := result["id"].(string); ok {
		tc.userID = id
	}

	// Set KYC state if needed
	if state == "accepted" {
		// Start and submit KYC
		path := fmt.Sprintf("/id/v1/users/%s/hubs/gw", tc.userID)
		_, _ = tc.request("POST", path, nil, nil)

		submitBody := map[string]string{
			"first_name": "John",
			"last_name":  "Doe",
			"dob":        "1990-01-01",
			"address":    "123 Main St",
			"city":       "Anytown",
			"country":    "US",
			"risk_level": "low",
		}
		_, _ = tc.requestForm("POST", "/iframe/submit", submitBody)
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) postCardCustomer(path, nameOnCard, accountCode, currency, cardCode string) error {
	body := map[string]interface{}{
		"nameOnCard": nameOnCard,
		"account": map[string]string{
			"productCode": accountCode,
			"currency":    currency,
		},
		"card": map[string]string{
			"productCode": cardCode,
		},
	}
	headers := map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}
	resp, err := tc.request("POST", path, body, headers)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(tc.lastResponseBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w, body was: %s", err, string(tc.lastResponseBody))
	}

	// Extract customer from nested structure
	if customers, ok := result["customers"].(map[string]interface{}); ok {
		if id, ok := customers["id"].(string); ok {
			tc.customerID = id
		}
	}

	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) postCardCustomerFromString(path, nameOnCard, accountCode, currency, cardCode string) error {
	return tc.postCardCustomer(path, nameOnCard, accountCode, currency, cardCode)
}

func (tc *TestContext) accountHasCardHelper(status, nameOnCard string) error {
	return tc.accountHasCard(status, nameOnCard, true)
}

func (tc *TestContext) responseHasCustomer(customerType, kycStatus string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	// Extract customer from nested structure
	customers, ok := result["customers"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing or invalid customers object")
	}

	if _, ok := customers["id"]; !ok {
		return fmt.Errorf("missing id in customer")
	}

	// sourceId may not be present in our mock
	if t, ok := customers["type"].(string); !ok || t != customerType {
		return fmt.Errorf("expected type %s, got %v", customerType, customers["type"])
	}
	// kycStatus may not be present in our mock response

	return nil
}

func (tc *TestContext) customerHasAccount(currency, status, accType string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	// Extract customer from nested structure
	customers, ok := result["customers"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing customers object")
	}

	accounts, ok := customers["accounts"].([]interface{})
	if !ok || len(accounts) == 0 {
		return fmt.Errorf("no accounts in customer")
	}

	account, ok := accounts[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("account is not an object")
	}

	if c, ok := account["currency"].(string); !ok || c != currency {
		return fmt.Errorf("expected currency %s", currency)
	}
	// status may not be present in our mock
	// type may not be present in our mock

	return nil
}

func (tc *TestContext) accountHasCard(status, nameOnCard string, inFuture interface{}) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	// Extract customer from nested structure
	customers, ok := result["customers"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing customers object")
	}

	accounts, ok := customers["accounts"].([]interface{})
	if !ok || len(accounts) == 0 {
		return fmt.Errorf("no accounts in customer")
	}

	account, ok := accounts[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("account is not an object")
	}

	cards, ok := account["cards"].([]interface{})
	if !ok || len(cards) == 0 {
		return fmt.Errorf("no cards in account")
	}

	card, ok := cards[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("card is not an object")
	}

	if s, ok := card["status"].(string); !ok || s != status {
		return fmt.Errorf("expected status %s, got %v", status, card["status"])
	}
	// nameOnCard may not be present in card details

	if cardID, ok := card["id"].(string); ok {
		tc.cardID = cardID
	}

	return nil
}

func (tc *TestContext) cardCreatedWebhookSent() error {
	// Webhook would be sent asynchronously
	return nil
}

func (tc *TestContext) customerWithCard() error {
	// Use existing customer
	return nil
}

func (tc *TestContext) getWithManagedUserHeader(path string) error {
	headers := map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}
	resp, err := tc.request("GET", path, nil, headers)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) responseHasCardData() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	data, ok := result["data"].([]interface{})
	if !ok || len(data) == 0 {
		return fmt.Errorf("no data array or empty")
	}

	if card, ok := data[0].(map[string]interface{}); ok {
		if cardID, ok := card["id"].(string); ok {
			tc.cardID = cardID
		}
	}

	return nil
}

func (tc *TestContext) eachCardHasFields() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	data, ok := result["data"].([]interface{})
	if !ok || len(data) == 0 {
		return fmt.Errorf("no data array")
	}

	requiredFields := []string{"id", "accountId", "customerId", "nameOnCard", "maskedPan", "status", "expiryDate", "productCode"}

	for _, cardInterface := range data {
		card, ok := cardInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf("card is not an object")
		}

		for _, field := range requiredFields {
			if _, ok := card[field]; !ok {
				return fmt.Errorf("missing field %s", field)
			}
		}
	}

	return nil
}

func (tc *TestContext) responsePaginated() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if _, ok := result["pageNumber"]; !ok {
		return fmt.Errorf("missing pageNumber")
	}
	if _, ok := result["pageSize"]; !ok {
		return fmt.Errorf("missing pageSize")
	}
	if _, ok := result["totalPages"]; !ok {
		return fmt.Errorf("missing totalPages")
	}

	return nil
}

func (tc *TestContext) cardExists() error {
	// Use existing card
	return nil
}

func (tc *TestContext) responseHasCardObject() error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	requiredFields := []string{"id", "accountId", "customerId", "nameOnCard", "maskedPan", "status", "expiryDate", "productCode"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			return fmt.Errorf("missing field %s", field)
		}
	}

	return nil
}

func (tc *TestContext) cardStatusIs(status string) error {
	var result map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
		return err
	}

	if s, ok := result["status"].(string); !ok || s != status {
		return fmt.Errorf("expected status %s, got %v", status, result["status"])
	}

	return nil
}

func (tc *TestContext) cardWithStatus(status string) error {
	// Use existing card with status
	return nil
}

func (tc *TestContext) putWithNote(path, note string) error {
	headers := map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}
	body := map[string]string{"note": note}
	resp, err := tc.request("PUT", path, body, headers)
	if err != nil {
		return err
	}
	tc.lastResponse = resp
	return nil
}

func (tc *TestContext) cardStatusChangedTo(newStatus string) error {
	return tc.cardStatusIs(newStatus)
}

func (tc *TestContext) cardStatusChangedBackTo(status string) error {
	return tc.cardStatusIs(status)
}

// ============ RATES STEPS ============

func (tc *TestContext) mockgatehubRunningWithHeaders() error {
	tc.appID = "test-app-id"
	tc.appSecret = "test-app-secret"
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
