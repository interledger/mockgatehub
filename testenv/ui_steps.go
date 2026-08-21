package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The admin UI is a browser-facing developer tool: it presents no HMAC
// credentials, so these steps deliberately send none.

func (tc *TestContext) browseTo(path string) error {
	_, err := tc.requestRaw(http.MethodGet, path, "", "", nil)
	return err
}

func (tc *TestContext) browseToUserDetail() error {
	return tc.browseTo("/ui/users/" + tc.userID)
}

func (tc *TestContext) browseToCardTxFormForUser() error {
	return tc.browseTo("/ui/actions/card-transaction?userID=" + url.QueryEscape(tc.userID))
}

// submitUIForm posts a form the way a browser would, but stops at the redirect
// instead of following it. Where the UI sends the user, and what it reports on
// the way, is the behaviour under test.
func (tc *TestContext) submitUIForm(path string, form url.Values) error {
	req, err := http.NewRequest(http.MethodPost, tc.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// A client that follows redirects would report the destination page and
	// hide the 303 and its message entirely.
	saved := tc.client.CheckRedirect
	tc.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { tc.client.CheckRedirect = saved }()

	_, err = tc.doRequest(req)
	return err
}

func (tc *TestContext) submitUIKYCAction(outcome string) error {
	return tc.submitUIForm("/ui/actions/kyc", url.Values{
		"userID":  {tc.userID},
		"outcome": {outcome},
		"message": {"sent from the e2e suite"},
	})
}

func (tc *TestContext) submitUICardTransaction(scenario string) error {
	return tc.submitUIForm("/ui/actions/card-transaction", url.Values{
		"userID":   {tc.userID},
		"cardID":   {tc.cardID},
		"scenario": {scenario},
	})
}

func (tc *TestContext) submitUIWithdrawalSettlement(event string) error {
	if tc.withdrawalID == "" {
		return fmt.Errorf("no withdrawal recorded to settle")
	}
	return tc.submitUIForm("/ui/actions/withdrawal/settle", url.Values{
		"userID": {tc.userID},
		"txID":   {tc.withdrawalID},
		"event":  {event},
	})
}

// ---- assertions ----

func (tc *TestContext) responseIsHTML() error {
	contentType := tc.lastResponse.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return fmt.Errorf("expected an HTML page, got Content-Type %q", contentType)
	}
	if !strings.Contains(string(tc.lastResponseBody), "<!DOCTYPE html>") {
		return fmt.Errorf("response is not an HTML document: %.80q", string(tc.lastResponseBody))
	}
	return nil
}

func (tc *TestContext) pageShows(text string) error {
	switch text {
	case "the user id":
		text = tc.userID
	case "the card id":
		text = tc.cardID
	}
	if !strings.Contains(string(tc.lastResponseBody), text) {
		return fmt.Errorf("page does not show %q", text)
	}
	return nil
}

func (tc *TestContext) pageDoesNotShow(text string) error {
	if strings.Contains(string(tc.lastResponseBody), text) {
		return fmt.Errorf("page unexpectedly shows %q", text)
	}
	return nil
}

// pageOffersEveryCardScenario checks the form is driven by the catalogue rather
// than requiring hand-written JSON.
func (tc *TestContext) pageOffersEveryCardScenario() error {
	savedResponse, savedBody := tc.lastResponse, tc.lastResponseBody

	if _, err := tc.request(http.MethodGet, "/admin/card-transactions/scenarios", nil, nil); err != nil {
		return err
	}
	var catalogue struct {
		Scenarios []struct {
			Key string `json:"key"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &catalogue); err != nil {
		return err
	}

	tc.lastResponse, tc.lastResponseBody = savedResponse, savedBody
	page := string(tc.lastResponseBody)

	if len(catalogue.Scenarios) == 0 {
		return fmt.Errorf("the catalogue is empty, so this assertion would be vacuous")
	}
	for _, scenario := range catalogue.Scenarios {
		if !strings.Contains(page, `value="`+scenario.Key+`"`) {
			return fmt.Errorf("the form does not offer scenario %q", scenario.Key)
		}
	}
	return nil
}

// uiRedirectedWithSuccess checks the UI sent the browser onward reporting
// success rather than reporting a failure.
func (tc *TestContext) uiRedirectedWithSuccess() error {
	return tc.checkUIRedirect(false)
}

func (tc *TestContext) uiRedirectedWithFailure() error {
	return tc.checkUIRedirect(true)
}

func (tc *TestContext) checkUIRedirect(wantError bool) error {
	if tc.lastResponse.StatusCode != http.StatusSeeOther {
		return fmt.Errorf("expected a redirect (303), got %d: %s",
			tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}

	location := tc.lastResponse.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("redirect carried no Location header")
	}
	tc.lastRedirect = location

	isError := strings.Contains(location, "error=1")
	if isError != wantError {
		return fmt.Errorf("expected error=%v in the redirect, got %q", wantError, location)
	}
	return nil
}

// uiRedirectReports checks the message the UI carried back to the user.
func (tc *TestContext) uiRedirectReports(text string) error {
	if tc.lastRedirect == "" {
		return fmt.Errorf("no redirect recorded")
	}
	decoded, err := url.QueryUnescape(tc.lastRedirect)
	if err != nil {
		decoded = tc.lastRedirect
	}
	if !strings.Contains(decoded, text) {
		return fmt.Errorf("the redirect does not report %q; it says %q", text, decoded)
	}
	return nil
}

// followUIRedirect loads the page the UI sent the browser to.
func (tc *TestContext) followUIRedirect() error {
	if tc.lastRedirect == "" {
		return fmt.Errorf("no redirect recorded")
	}
	target := tc.lastRedirect
	if idx := strings.Index(target, tc.baseURL); idx == 0 {
		target = target[len(tc.baseURL):]
	}
	return tc.browseTo(target)
}
