package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (h *harness) bootstrapAcceptedUser(email string) (string, error) {
	userID, err := h.createManagedUser(email)
	if err != nil {
		return "", err
	}
	if err := h.startKYC(userID); err != nil {
		return "", err
	}
	if err := h.submitKYC(userID); err != nil {
		return "", err
	}
	return userID, nil
}

func (h *harness) createManagedUser(email string) (string, error) {
	body := map[string]string{"email": email}
	resp := map[string]interface{}{}
	if err := h.postJSON("/auth/v1/users/managed", body, nil, &resp); err != nil {
		return "", err
	}
	id, _ := resp["id"].(string)
	if id == "" {
		return "", fmt.Errorf("no id in response")
	}
	return id, nil
}

func (h *harness) getIframeToken(userID string) (string, error) {
	body := map[string]interface{}{"scope": []string{"deposit"}}
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	resp := map[string]interface{}{}
	if err := h.postJSON("/auth/v1/tokens?clientId=test-client-id", body, headers, &resp); err != nil {
		return "", err
	}
	token, _ := resp["token"].(string)
	if token == "" {
		return "", fmt.Errorf("iframe token missing")
	}
	return token, nil
}

func (h *harness) startKYC(userID string) error {
	return h.postJSON(fmt.Sprintf("/id/v1/users/%s/hubs/gw", userID), map[string]string{}, nil, &map[string]interface{}{})
}

func (h *harness) submitKYC(userID string) error {
	data := url.Values{}
	data.Set("user_id", userID)
	data.Set("first_name", "Test")
	data.Set("last_name", "User")
	data.Set("dob", "1990-01-01")
	data.Set("address", "123 Main St")
	data.Set("city", "NY")
	data.Set("country", "USA")
	data.Set("risk_level", "low")

	req, err := http.NewRequest("POST", mockGatehubURL+"/iframe/submit", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthHeaders(req, nil)

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (h *harness) getUser(userID string) (*userResponse, error) {
	var user userResponse
	if err := h.getJSON(fmt.Sprintf("/id/v1/users/%s", userID), &user, nil); err != nil {
		return nil, err
	}
	return &user, nil
}

func (h *harness) createWallet(userID, name string) (*walletResponse, error) {
	body := map[string]string{"name": name}
	var wallet walletResponse
	if err := h.postJSON(fmt.Sprintf("/core/v1/users/%s/wallets", userID), body, nil, &wallet); err != nil {
		return nil, err
	}
	if wallet.Address == "" {
		return nil, fmt.Errorf("wallet address missing")
	}
	return &wallet, nil
}

func (h *harness) getBalances(address string) ([]balanceEntry, error) {
	var balances []balanceEntry
	if err := h.getJSON(fmt.Sprintf("/core/v1/wallets/%s/balances", address), &balances, nil); err != nil {
		return nil, err
	}
	return balances, nil
}

func (h *harness) createTransaction(req transactionRequest) (*transactionResponse, error) {
	var tx transactionResponse
	if err := h.postJSON("/core/v1/transactions", req, nil, &tx); err != nil {
		return nil, err
	}
	if tx.ID == "" {
		tx.ID = tx.UUID
	}
	if tx.ID == "" {
		return nil, fmt.Errorf("transaction id missing")
	}
	return &tx, nil
}

// Cards helpers
func (h *harness) createCustomerAndCard(userID, walletAddress, nameOnCard string) (*cardCustomerResponse, error) {
	body := map[string]interface{}{
		"walletAddress": walletAddress,
		"nameOnCard":    nameOnCard,
		"account": map[string]interface{}{
			"productCode": "PWSR_DEBP_2404",
			"currency":    "EUR",
			"card": map[string]interface{}{
				"productCode": "PWSR_DEBP_2404",
			},
		},
	}
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp cardCustomerResponse
	if err := h.postJSON("/cards/v1/customers", body, headers, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *harness) createDeliveryAddress(userID, customerID string) (*cardAddress, error) {
	body := map[string]interface{}{
		"type":        "DELIVERY",
		"countryCode": "USA",
		"line1":       "123 Main St",
		"city":        "NYC",
		"zipCode":     "10001",
		"reason":      "shipping",
	}
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp []cardAddress
	path := fmt.Sprintf("/cards/v1/customers/%s/addresses", customerID)
	if err := h.postJSON(path, body, headers, &resp); err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, fmt.Errorf("no address returned")
	}
	return &resp[len(resp)-1], nil
}

func (h *harness) orderAdditionalCard(userID, accountID, walletAddress string) (*cardItem, error) {
	body := map[string]interface{}{
		"currency":      "EUR",
		"productCode":   "PWSR_DEBP_2404",
		"nameOnCard":    "Test User",
		"walletAddress": walletAddress,
		"card": map[string]interface{}{
			"productCode": "PWSR_DEBP_2404",
		},
	}
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp cardItem
	path := fmt.Sprintf("/cards/v1/cards/%s/card", accountID)
	if err := h.postJSON(path, body, headers, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *harness) getCard(userID, cardID string) (*cardItem, error) {
	var card cardItem
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	path := fmt.Sprintf("/cards/v1/cards/%s/card", cardID)
	if err := h.getJSON(path, &card, headers); err != nil {
		return nil, err
	}
	return &card, nil
}

func (h *harness) getCardLimits(userID, cardID string) ([]cardLimit, error) {
	var resp []cardLimit
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	path := fmt.Sprintf("/cards/v1/cards/%s/limits", cardID)
	if err := h.getJSON(path, &resp, headers); err != nil {
		return nil, err
	}
	return resp, nil
}

func (h *harness) updateCardLimits(userID, cardID string, limits []cardLimit) error {
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	path := fmt.Sprintf("/cards/v1/cards/%s/limits", cardID)
	return h.putJSON(path, limits, headers, nil)
}

func (h *harness) getCardToken(userID, cardID string) (string, error) {
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp cardTokenResponse
	body := map[string]interface{}{"cardId": cardID}
	if err := h.postJSON("/cards/v1/token/card-data", body, headers, &resp); err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", fmt.Errorf("token missing")
	}
	return resp.Token, nil
}

func (h *harness) createCardTransaction(userID, cardID string) (*cardTransactionResponse, error) {
	body := map[string]interface{}{
		"cardId":       cardID,
		"amount":       "12.50",
		"currency":     "EUR",
		"type":         1,
		"merchantName": "Test Store",
	}
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp cardTransactionResponse
	if err := h.postJSON("/cards/v1/transactions", body, headers, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *harness) listCardTransactions(userID, cardID string) ([]cardTransactionResponse, error) {
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp struct {
		Data []cardTransactionResponse `json:"data"`
	}
	path := fmt.Sprintf("/cards/v1/cards/%s/transactions", cardID)
	if err := h.getJSON(path, &resp, headers); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (h *harness) createThreeDSChallenge(userID, cardID string) (string, error) {
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	body := map[string]interface{}{
		"cardId":           cardID,
		"merchantName":     "Test Merchant",
		"purchaseAmount":   "15.00",
		"purchaseCurrency": "EUR",
		"timeoutMinutes":   5,
	}
	var resp map[string]interface{}
	if err := h.postJSON("/cards/v1/test/3ds/challenge", body, headers, &resp); err != nil {
		return "", err
	}
	txID, _ := resp["transactionId"].(string)
	if txID == "" {
		return "", fmt.Errorf("transactionId missing in 3ds challenge")
	}
	return txID, nil
}

func (h *harness) getPendingConfirmations(userID string) (*pending3DSResponse, error) {
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	var resp pending3DSResponse
	if err := h.getJSON("/cards/v1/transaction/pending-confirmations", &resp, headers); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *harness) confirmThreeDS(userID, txID string, approve bool) error {
	headers := map[string]string{"x-gatehub-managed-user-uuid": userID}
	body := map[string]interface{}{
		"confirmed":  approve,
		"authMethod": "password",
	}
	path := fmt.Sprintf("/cards/v1/transaction/%s", txID)
	return h.postJSON(path, body, headers, &map[string]interface{}{})
}

func (h *harness) completeDeposit(iframeToken string, amount float64, currency string) error {
	body := map[string]interface{}{
		"amount":   fmt.Sprintf("%.2f", amount),
		"currency": currency,
	}
	headers := map[string]string{
		"Authorization": "Bearer " + iframeToken,
	}
	var resp map[string]interface{}
	path := fmt.Sprintf("/transaction/complete?paymentType=deposit&bearer=%s", url.QueryEscape(iframeToken))
	if err := h.postJSON(path, body, headers, &resp); err != nil {
		return err
	}
	status, _ := resp["status"].(string)
	if status != "success" {
		return fmt.Errorf("unexpected status %s", status)
	}
	return nil
}
