package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// usingTheAsyncWithdrawalsInstance points subsequent requests at the instance
// configured with MOCKGATEHUB_ASYNC_WITHDRAWALS enabled. Both positions of the
// switch need covering, and the default instance must keep settling
// immediately.
func (tc *TestContext) usingTheAsyncWithdrawalsInstance() error {
	tc.baseURL = asyncWithdrawalsURL
	// Its admin listener has to move with it, or admin calls would land on the
	// other instance and see none of this one's state.
	tc.adminBaseURL = asyncWithdrawalsAdminURL
	return nil
}

// requestWithdrawal drives the iframe withdrawal completion route and records
// the transaction id so later steps can settle it.
func (tc *TestContext) requestWithdrawal(amount, currency string) error {
	if tc.iframeToken == "" {
		if err := tc.iframeTokenWithScope("withdraw"); err != nil {
			return err
		}
	}

	body, err := json.Marshal(map[string]string{"amount": amount, "currency": currency})
	if err != nil {
		return err
	}

	if _, err := tc.requestRaw(http.MethodPost,
		"/transaction/complete?paymentType=withdraw", string(body), "application/json",
		map[string]string{"Authorization": "Bearer " + tc.iframeToken}); err != nil {
		return err
	}

	if tc.lastResponse.StatusCode != http.StatusOK {
		// Leave the response for the scenario to assert on.
		return nil
	}

	var resp map[string]string
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return fmt.Errorf("could not parse withdrawal response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	tc.withdrawalID = resp["transaction_id"]
	if tc.withdrawalID == "" {
		return fmt.Errorf("withdrawal response carried no transaction_id: %s", string(tc.lastResponseBody))
	}
	return nil
}

// withdrawalStatusIs reads the withdrawal back through the listing and checks
// the status a consumer would observe.
func (tc *TestContext) withdrawalStatusIs(expected string) error {
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody
	defer func() { tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody }()

	withdrawals, err := tc.listWithdrawals("")
	if err != nil {
		return err
	}

	wantStatus, ok := map[string]float64{"pending": 1, "completed": 100, "failed": 3}[expected]
	if !ok {
		return fmt.Errorf("unknown expected status %q", expected)
	}

	for _, wd := range withdrawals {
		if wd["uuid"] != tc.withdrawalID {
			continue
		}
		if wd["status"] != wantStatus {
			return fmt.Errorf("expected withdrawal %s to be %s (%v), got %v",
				tc.withdrawalID, expected, wantStatus, wd["status"])
		}
		return nil
	}
	return fmt.Errorf("withdrawal %s not found in the listing", tc.withdrawalID)
}

func (tc *TestContext) listWithdrawals(status string) ([]map[string]interface{}, error) {
	path := "/admin/users/" + tc.userID + "/withdrawals"
	if status != "" {
		path += "?status=" + status
	}
	if _, err := tc.request(http.MethodGet, path, nil, nil); err != nil {
		return nil, err
	}
	if tc.lastResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing withdrawals failed with status %d: %s",
			tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}

	var resp struct {
		Withdrawals []map[string]interface{} `json:"withdrawals"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return nil, fmt.Errorf("could not parse withdrawal listing: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	return resp.Withdrawals, nil
}

// iGETPendingWithdrawals leaves the listing as the response under assertion.
func (tc *TestContext) iGETWithdrawalsFiltered(status string) error {
	path := "/admin/users/" + tc.userID + "/withdrawals"
	if status != "" {
		path += "?status=" + status
	}
	_, err := tc.request(http.MethodGet, path, nil, nil)
	return err
}

func (tc *TestContext) withdrawalListingHolds(count int) error {
	var resp struct {
		Withdrawals []map[string]interface{} `json:"withdrawals"`
		Count       int                      `json:"count"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return fmt.Errorf("could not parse listing: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	if len(resp.Withdrawals) != count || resp.Count != count {
		return fmt.Errorf("expected %d withdrawal(s), got %d (count field %d)", count, len(resp.Withdrawals), resp.Count)
	}
	return nil
}

// withdrawalListingNamesTheBankDetails checks a consumer can say where the
// money went.
func (tc *TestContext) withdrawalListingNamesTheBankDetails() error {
	var resp struct {
		Withdrawals []map[string]interface{} `json:"withdrawals"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return err
	}
	if len(resp.Withdrawals) == 0 {
		return fmt.Errorf("no withdrawals in the listing")
	}
	for _, field := range []string{"account_iban", "account_legal_name", "message"} {
		if value, ok := resp.Withdrawals[0][field]; !ok || value == "" {
			return fmt.Errorf("withdrawal does not carry %q: %v", field, resp.Withdrawals[0])
		}
	}
	return nil
}

// triggerWithdrawalEvent settles the recorded withdrawal.
func (tc *TestContext) triggerWithdrawalEvent(event string) error {
	if tc.withdrawalID == "" {
		return fmt.Errorf("no withdrawal recorded to settle")
	}
	body := map[string]string{"event": event}
	_, err := tc.request(http.MethodPost, "/admin/withdrawals/"+tc.withdrawalID+"/trigger-event", body, nil)
	return err
}

func (tc *TestContext) triggerWithdrawalEventFor(txID, event string) error {
	body := map[string]string{"event": event}
	_, err := tc.request(http.MethodPost, "/admin/withdrawals/"+txID+"/trigger-event", body, nil)
	return err
}

// withdrawalWebhookReportsTheTransaction checks the settlement webhook names
// the withdrawal it settles, which is how a consumer reconciles it.
func (tc *TestContext) withdrawalWebhookReportsTheTransaction(event string) error {
	payload, err := tc.awaitWebhook(event)
	if err != nil {
		return err
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("webhook has no data object: %v", payload)
	}
	if data["tx_uuid"] != tc.withdrawalID {
		return fmt.Errorf("webhook reported transaction %v, expected %s", data["tx_uuid"], tc.withdrawalID)
	}
	for _, field := range []string{"amount", "currency"} {
		if _, ok := data[field]; !ok {
			return fmt.Errorf("withdrawal webhook is missing %q: %v", field, data)
		}
	}
	return nil
}
