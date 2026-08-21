package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// simulateCardTransaction drives the simulation endpoint for one catalogue
// scenario against the card the scenario set up.
func (tc *TestContext) simulateCardTransaction(scenario string) error {
	body := map[string]interface{}{
		"userId":   tc.userID,
		"cardId":   tc.cardID,
		"scenario": scenario,
	}
	if _, err := tc.request(http.MethodPost, "/admin/card-transactions/simulate", body, nil); err != nil {
		return err
	}
	return tc.captureSimulatedTransaction()
}

// simulateCardTransactionWithEvent selects which webhook the simulation emits.
func (tc *TestContext) simulateCardTransactionWithEvent(scenario, event string) error {
	body := map[string]interface{}{
		"userId":   tc.userID,
		"cardId":   tc.cardID,
		"scenario": scenario,
		"event":    event,
	}
	if _, err := tc.request(http.MethodPost, "/admin/card-transactions/simulate", body, nil); err != nil {
		return err
	}
	return tc.captureSimulatedTransaction()
}

// simulateNCardTransactions creates several transactions so paging is
// observable, and fails if any of them is rejected.
func (tc *TestContext) simulateNCardTransactions(count int, scenario string) error {
	for i := 0; i < count; i++ {
		body := map[string]interface{}{
			"userId":    tc.userID,
			"cardId":    tc.cardID,
			"scenario":  scenario,
			"overrides": map[string]interface{}{"transactionAmount": fmt.Sprintf("%d.00", i+1)},
		}
		if _, err := tc.request(http.MethodPost, "/admin/card-transactions/simulate", body, nil); err != nil {
			return err
		}
		if tc.lastResponse.StatusCode != http.StatusCreated {
			return fmt.Errorf("simulation %d failed with status %d: %s", i+1, tc.lastResponse.StatusCode, string(tc.lastResponseBody))
		}
		if err := tc.captureSimulatedTransaction(); err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) captureSimulatedTransaction() error {
	if tc.lastResponse.StatusCode != http.StatusCreated {
		// Leave the error response in place for the scenario to assert on.
		return nil
	}
	var resp struct {
		TransactionID string `json:"transactionId"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return fmt.Errorf("could not parse simulate response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	tc.simulatedTxID = resp.TransactionID
	tc.simulatedTxIDs = append(tc.simulatedTxIDs, resp.TransactionID)
	return nil
}

// simulatedTransactionHasNumericIdentifiers asserts on the two fields that were
// previously left null and that consumers key off.
func (tc *TestContext) simulatedTransactionHasNumericIdentifiers() error {
	tx, err := tc.simulatedTransactionBody()
	if err != nil {
		return err
	}

	id, ok := tx["id"].(float64)
	if !ok {
		return fmt.Errorf("transaction has no numeric id: %v", tx["id"])
	}
	if id <= 0 {
		return fmt.Errorf("expected a positive transaction id, got %v", id)
	}

	cardID, ok := tx["cardId"].(float64)
	if !ok {
		return fmt.Errorf("transaction has no numeric cardId: %v", tx["cardId"])
	}
	if cardID <= 0 {
		return fmt.Errorf("expected a positive cardId, got %v", cardID)
	}
	return nil
}

func (tc *TestContext) simulatedTransactionBody() (map[string]interface{}, error) {
	var resp struct {
		Transaction map[string]interface{} `json:"transaction"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return nil, fmt.Errorf("could not parse simulate response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	if resp.Transaction == nil {
		return nil, fmt.Errorf("simulate response carried no transaction: %s", string(tc.lastResponseBody))
	}
	return resp.Transaction, nil
}

// simulatedTransactionAppearsInListingWithUnmodelledFields is the point of
// storing the raw payload: fields this service does not model must still reach
// the consumer through the listing.
func (tc *TestContext) simulatedTransactionAppearsInListingWithUnmodelledFields() error {
	var listing struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &listing); err != nil {
		return fmt.Errorf("could not parse listing: %w. Body: %s", err, string(tc.lastResponseBody))
	}

	for _, tx := range listing.Data {
		if tx["transactionId"] != tc.simulatedTxID {
			continue
		}
		for _, field := range []string{"mcc", "merchantStreet", "merchantZip", "terminalId", "source"} {
			if _, ok := tx[field]; !ok {
				return fmt.Errorf("listed transaction lost the %q field: %v", field, tx)
			}
		}
		return nil
	}
	return fmt.Errorf("simulated transaction %s did not appear in the listing", tc.simulatedTxID)
}

func (tc *TestContext) listingHoldsPage(pageCount, totalRecords, totalPages int) error {
	var listing struct {
		Data       []map[string]interface{} `json:"data"`
		Pagination struct {
			TotalRecords int `json:"totalRecords"`
			TotalPages   int `json:"totalPages"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &listing); err != nil {
		return fmt.Errorf("could not parse listing: %w. Body: %s", err, string(tc.lastResponseBody))
	}

	if len(listing.Data) != pageCount {
		return fmt.Errorf("expected %d transactions on the page, got %d", pageCount, len(listing.Data))
	}
	if listing.Pagination.TotalRecords != totalRecords {
		return fmt.Errorf("expected totalRecords %d, got %d", totalRecords, listing.Pagination.TotalRecords)
	}
	if listing.Pagination.TotalPages != totalPages {
		return fmt.Errorf("expected totalPages %d, got %d", totalPages, listing.Pagination.TotalPages)
	}
	return nil
}

// setSimulatedTransactionStatus advances the transaction created earlier.
func (tc *TestContext) setSimulatedTransactionStatus(status string) error {
	if tc.simulatedTxID == "" {
		return fmt.Errorf("no simulated transaction to update")
	}
	body := map[string]string{"status": status}
	_, err := tc.request(http.MethodPost, "/admin/card-transactions/"+tc.simulatedTxID+"/status", body, nil)
	return err
}

// storedTransactionStatusIs reads the transaction back and checks the status the
// consumer would see.
func (tc *TestContext) storedTransactionStatusIs(expected string) error {
	if _, err := tc.sendWithManagedUserHeader(http.MethodGet, "/cards/v1/transactions/"+tc.simulatedTxID, nil); err != nil {
		return err
	}
	var tx map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &tx); err != nil {
		return fmt.Errorf("could not parse transaction: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	if tx["txStatus"] != expected {
		return fmt.Errorf("expected txStatus %q, got %v", expected, tx["txStatus"])
	}
	return nil
}

func (tc *TestContext) getCardTransactionsPage(query string) error {
	path := "/cards/v1/cards/" + tc.cardID + "/transactions"
	if query != "" {
		path += "?" + query
	}
	_, err := tc.sendWithManagedUserHeader(http.MethodGet, path, nil)
	return err
}

// ---- catalogue ----

func (tc *TestContext) getCardTxScenarioCatalogue() error {
	_, err := tc.request(http.MethodGet, "/admin/card-transactions/scenarios", nil, nil)
	return err
}

func (tc *TestContext) catalogueListsScenarios(count int) error {
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return err
	}
	if resp.Count != count {
		return fmt.Errorf("expected %d scenarios in the catalogue, got %d", count, resp.Count)
	}
	return nil
}

func (tc *TestContext) catalogueIncludesScenario(key string) error {
	var resp struct {
		Scenarios []struct {
			Key string `json:"key"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return err
	}
	for _, s := range resp.Scenarios {
		if s.Key == key {
			return nil
		}
	}
	return fmt.Errorf("catalogue does not offer %q", key)
}

func (tc *TestContext) errorNamesTheValidScenarios() error {
	var resp struct {
		ValidScenarios []string `json:"validScenarios"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return err
	}
	if len(resp.ValidScenarios) == 0 {
		return fmt.Errorf("the rejection did not say which scenarios are valid: %s", string(tc.lastResponseBody))
	}
	return nil
}

// ---- webhook assertions ----

// webhookDeliveryTimeout allows for the queue's minimum delay before a webhook
// becomes eligible for delivery.
const webhookDeliveryTimeout = 30 * time.Second

func (tc *TestContext) clearReceivedWebhooks() error {
	_, err := tc.request(http.MethodDelete, "/admin/received-webhooks", nil, nil)
	return err
}

// awaitWebhook polls the sink until a delivery of the given event arrives for
// the current user, and returns it.
func (tc *TestContext) awaitWebhook(event string) (map[string]interface{}, error) {
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody
	defer func() { tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody }()

	deadline := time.Now().Add(webhookDeliveryTimeout)
	for time.Now().Before(deadline) {
		path := fmt.Sprintf("/admin/received-webhooks?event=%s&user=%s", event, tc.userID)
		if _, err := tc.request(http.MethodGet, path, nil, nil); err != nil {
			return nil, err
		}

		var resp struct {
			Webhooks []struct {
				EventType      string                 `json:"event_type"`
				SignatureValid bool                   `json:"signature_valid"`
				Payload        map[string]interface{} `json:"payload"`
			} `json:"webhooks"`
		}
		if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
			return nil, fmt.Errorf("could not parse webhook sink response: %w. Body: %s", err, string(tc.lastResponseBody))
		}

		if len(resp.Webhooks) > 0 {
			hook := resp.Webhooks[len(resp.Webhooks)-1]
			if !hook.SignatureValid {
				return nil, fmt.Errorf("webhook %s arrived without a valid signature", event)
			}
			return hook.Payload, nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return nil, fmt.Errorf("no %s webhook was delivered for user %s within %s", event, tc.userID, webhookDeliveryTimeout)
}

func (tc *TestContext) webhookIsDelivered(event string) error {
	_, err := tc.awaitWebhook(event)
	return err
}

// cardTransactionWebhookCarriesTheTransaction is the behaviour that makes the
// event usable: a consumer mirroring card spend needs the amounts, not just an
// identifier.
func (tc *TestContext) cardTransactionWebhookCarriesTheTransaction(event string) error {
	payload, err := tc.awaitWebhook(event)
	if err != nil {
		return err
	}

	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("webhook payload has no data object: %v", payload)
	}
	authorizationData, ok := data["authorizationData"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("webhook data carries no authorizationData: %v", data)
	}

	for _, field := range []string{"transactionId", "billingAmount", "billingCurrency", "type", "id", "cardId"} {
		if _, ok := authorizationData[field]; !ok {
			return fmt.Errorf("authorizationData is missing %q: %v", field, authorizationData)
		}
	}
	if authorizationData["transactionId"] != tc.simulatedTxID {
		return fmt.Errorf("webhook reported transaction %v, expected %s", authorizationData["transactionId"], tc.simulatedTxID)
	}
	return nil
}

// cardCreatedWebhookCarriesTheIdentifiers checks the consumer is told which ids
// were assigned, which is the reason the event exists.
func (tc *TestContext) cardCreatedWebhookCarriesTheIdentifiers() error {
	payload, err := tc.awaitWebhook("cards.card.created")
	if err != nil {
		return err
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("webhook payload has no data object: %v", payload)
	}
	for _, field := range []string{"cardId", "customerId", "accountId", "maskedPan", "nameOnCard", "productCode"} {
		value, ok := data[field]
		if !ok {
			return fmt.Errorf("cards.card.created is missing %q: %v", field, data)
		}
		if value == nil || value == "" {
			return fmt.Errorf("cards.card.created has an empty %q", field)
		}
	}
	if data["cardId"] != tc.cardID {
		return fmt.Errorf("webhook reported card %v, expected %s", data["cardId"], tc.cardID)
	}
	return nil
}
