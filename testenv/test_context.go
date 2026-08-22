package main

import (
	"net/http"
	"time"
)

// TestContext holds the state for BDD test scenarios
type TestContext struct {
	// HTTP client
	client *http.Client

	// Base configuration. adminBaseURL addresses the separate listener that
	// serves the admin UI and the test-support endpoints.
	baseURL      string
	adminBaseURL string

	// Authentication
	appID     string
	appSecret string

	// Signature authentication fields
	signatureTime     string
	signatureBody     string
	signatureBase     string
	signatureURL      string
	signatureOverride string
	signatureHeaders  map[string]string

	// Pending request (for multi-step signature tests)
	pendingMethod string
	pendingPath   string
	pendingBody   string
	pendingMode   string

	// Response storage
	lastResponse     *http.Response
	lastResponseBody []byte
	lastError        error

	// Test data
	userID          string
	walletAddress   string
	previousAddress string
	accessToken     string
	iframeToken     string
	kycToken        string
	email           string
	customerID      string
	cardID          string
	transactionID   string
	savedRates      map[string]float64

	// recordedBalances holds balance snapshots taken mid-scenario so later
	// steps can assert on the change rather than on an absolute figure.
	recordedBalances map[string]float64

	// Card token / crypto state
	keys            *callerKeys
	cardToken       string
	cardTokenHref   string
	cardTokenMethod string
	decryptedPAN    string

	// Card transaction simulation state
	simulatedTxID  string
	simulatedTxIDs []string

	// statementPeriod records the period a statement was requested for, so a
	// later step can assert the document names it.
	statementPeriod time.Time

	// withdrawalID is the withdrawal a scenario is settling.
	withdrawalID string

	// lastRedirect is where the admin UI last sent the browser.
	lastRedirect string
}

// Reset initializes the test context to a clean state
func (tc *TestContext) Reset() {
	tc.client = &http.Client{Timeout: 10 * time.Second}
	tc.baseURL = mockGatehubURL
	tc.adminBaseURL = mockGatehubAdminURL
	tc.appID = ""
	tc.appSecret = ""
	tc.signatureTime = ""
	tc.signatureBody = ""
	tc.signatureBase = ""
	tc.signatureURL = ""
	tc.signatureOverride = ""
	tc.signatureHeaders = nil
	tc.pendingMethod = ""
	tc.pendingPath = ""
	tc.pendingBody = ""
	tc.pendingMode = ""
	tc.userID = ""
	tc.walletAddress = ""
	tc.accessToken = ""
	tc.iframeToken = ""
	tc.kycToken = ""
	tc.customerID = ""
	tc.cardID = ""
	tc.transactionID = ""
	tc.email = ""
	tc.previousAddress = ""
	tc.lastResponse = nil
	tc.lastResponseBody = nil
	tc.lastError = nil
	tc.savedRates = make(map[string]float64)
	tc.recordedBalances = nil
	tc.keys = nil
	tc.cardToken = ""
	tc.cardTokenHref = ""
	tc.cardTokenMethod = ""
	tc.decryptedPAN = ""
	tc.simulatedTxID = ""
	tc.simulatedTxIDs = nil
	tc.statementPeriod = time.Time{}
	tc.withdrawalID = ""
	tc.lastRedirect = ""
}
