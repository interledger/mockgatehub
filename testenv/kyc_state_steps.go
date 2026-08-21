package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// submitKYCFormWithOutcome drives the iframe KYC form asking for a specific
// verdict, which is how a scenario exercises anything other than the happy path.
func (tc *TestContext) submitKYCFormWithOutcome(outcome string) error {
	formData := map[string]string{
		"user_id":     tc.userID,
		"first_name":  "Jane",
		"last_name":   "Doe",
		"dob":         "1990-05-15",
		"address":     "123 Main St",
		"city":        "Testville",
		"country":     "US",
		"risk_level":  "low",
		"kyc_outcome": outcome,
	}
	_, err := tc.requestForm(http.MethodPost, "/iframe/submit", formData)
	return err
}

// setKYCStateQuietly arranges a starting state without the arrangement itself
// looking like an event to the consumer.
func (tc *TestContext) setKYCStateQuietly(state string) error {
	body := map[string]string{"kyc_state": state}
	_, err := tc.request(http.MethodPut, "/admin/users/"+tc.userID+"/kyc-state", body, nil)
	return err
}

// userVerificationReports checks the numeric pair consumers branch on, rather
// than only the human-readable kyc_state beside it.
func (tc *TestContext) userVerificationReports(status, state int) error {
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody
	defer func() { tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody }()

	if _, err := tc.request(http.MethodGet, "/id/v1/users/"+tc.userID, nil, nil); err != nil {
		return err
	}

	var resp struct {
		Verifications []struct {
			Status       int    `json:"status"`
			State        int    `json:"state"`
			Provider     string `json:"provider"`
			ProviderType string `json:"provider_type"`
		} `json:"verifications"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return fmt.Errorf("could not parse user response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	if len(resp.Verifications) == 0 {
		return fmt.Errorf("user reports no verifications: %s", string(tc.lastResponseBody))
	}

	v := resp.Verifications[0]
	if v.Status != status || v.State != state {
		return fmt.Errorf("expected verification status/state %d/%d, got %d/%d", status, state, v.Status, v.State)
	}
	if v.Provider == "" || v.ProviderType == "" {
		return fmt.Errorf("verification does not name its provider: %+v", v)
	}
	return nil
}

// userReportsSameIdUnderBothNames guards the dual-spelling contract.
func (tc *TestContext) userReportsSameIdUnderBothNames() error {
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody
	defer func() { tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody }()

	if _, err := tc.request(http.MethodGet, "/id/v1/users/"+tc.userID, nil, nil); err != nil {
		return err
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return err
	}
	id, hasID := resp["id"].(string)
	uuid, hasUUID := resp["uuid"].(string)
	if !hasID || !hasUUID {
		return fmt.Errorf("expected both id and uuid, got id=%v uuid=%v", resp["id"], resp["uuid"])
	}
	if id != tc.userID || uuid != tc.userID {
		return fmt.Errorf("expected both to be %s, got id=%s uuid=%s", tc.userID, id, uuid)
	}
	return nil
}

// kycWebhookCarriesVerdict asserts the webhook's verified summary, which is what
// a consumer keys off rather than the message text.
func (tc *TestContext) kycWebhookCarriesVerdict(event, short string, status int) error {
	payload, err := tc.awaitWebhook(event)
	if err != nil {
		return err
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("webhook has no data object: %v", payload)
	}
	verified, ok := data["verified"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s carries no verified summary: %v", event, data)
	}
	if verified["short"] != short {
		return fmt.Errorf("expected verified.short %q, got %v", short, verified["short"])
	}
	if int(verified["status"].(float64)) != status {
		return fmt.Errorf("expected verified.status %d, got %v", status, verified["status"])
	}
	return nil
}

// kycWebhookCarriesNoVerdict asserts the opposite: an event that is not a
// verdict must not look like one.
func (tc *TestContext) kycWebhookCarriesNoVerdict(event string) error {
	payload, err := tc.awaitWebhook(event)
	if err != nil {
		return err
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("webhook has no data object: %v", payload)
	}
	if _, present := data["verified"]; present {
		return fmt.Errorf("%s should not carry a verification verdict, but has %v", event, data["verified"])
	}
	if data["gateway"] == nil {
		return fmt.Errorf("%s should still name the identity gateway: %v", event, data)
	}
	return nil
}

// noWebhookIsDelivered gives the queue time to deliver anything it was going to
// and then asserts nothing arrived for the user.
func (tc *TestContext) noWebhookIsDelivered() error {
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody
	defer func() { tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody }()

	// The queue holds jobs for a minimum delay before delivering, so a check
	// made immediately would pass even if a webhook were on its way.
	time.Sleep(6 * time.Second)

	if _, err := tc.request(http.MethodGet, "/admin/received-webhooks?user="+tc.userID, nil, nil); err != nil {
		return err
	}
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return err
	}
	if resp.Count != 0 {
		return fmt.Errorf("expected no webhooks for user %s, but %d arrived: %s", tc.userID, resp.Count, string(tc.lastResponseBody))
	}
	return nil
}
