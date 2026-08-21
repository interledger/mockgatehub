package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// pdfDrawnText matches the strings a PDF content stream draws, which is what a
// reader opening the document would see.
var pdfDrawnText = regexp.MustCompile(`\(((?:[^()\\]|\\.)*)\) Tj`)

// statementText renders the last response as the text a reader would see.
func (tc *TestContext) statementText() string {
	matches := pdfDrawnText.FindAllStringSubmatch(string(tc.lastResponseBody), -1)
	lines := make([]string, 0, len(matches))
	unescape := strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`)
	for _, m := range matches {
		lines = append(lines, unescape.Replace(m[1]))
	}
	return strings.Join(lines, "\n")
}

func (tc *TestContext) getAccountConfirmation() error {
	_, err := tc.request(http.MethodGet, "/statement/v1/statements/account-confirmation/"+tc.walletAddress, nil, nil)
	return err
}

// getAccountConfirmationUnauthenticated sends no HMAC headers at all, to check
// statements are not accidentally public.
func (tc *TestContext) getAccountConfirmationUnauthenticated() error {
	_, err := tc.requestRaw(http.MethodGet, "/statement/v1/statements/account-confirmation/"+tc.walletAddress, "", "", nil)
	return err
}

func (tc *TestContext) getAccountStatementForCurrentMonth() error {
	now := time.Now().UTC()
	tc.statementPeriod = now
	path := fmt.Sprintf("/statement/v1/statements/account-statement/%s/%d/%d",
		tc.walletAddress, now.Year(), int(now.Month()))
	_, err := tc.request(http.MethodGet, path, nil, nil)
	return err
}

func (tc *TestContext) getAccountStatementForPeriod(period string) error {
	path := "/statement/v1/statements/account-statement/" + tc.walletAddress + "/" + period
	_, err := tc.request(http.MethodGet, path, nil, nil)
	return err
}

func (tc *TestContext) getTransferConfirmationForLastTransaction() error {
	if tc.transactionID == "" {
		return fmt.Errorf("no transaction recorded to confirm")
	}
	return tc.getTransferConfirmationFor(tc.transactionID)
}

func (tc *TestContext) getTransferConfirmationFor(txID string) error {
	_, err := tc.request(http.MethodGet, "/statement/v1/statements/transfer-confirmation/"+txID, nil, nil)
	return err
}

// ---- assertions ----

func (tc *TestContext) responseIsPDFAttachment(filename string) error {
	contentType := tc.lastResponse.Header.Get("Content-Type")
	if contentType != "application/pdf" {
		return fmt.Errorf("expected Content-Type application/pdf, got %q", contentType)
	}

	disposition := tc.lastResponse.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, filename) {
		return fmt.Errorf("expected Content-Disposition to name %q, got %q", filename, disposition)
	}

	if !strings.HasPrefix(string(tc.lastResponseBody), "%PDF-") {
		return fmt.Errorf("body is not a PDF: %.60q", string(tc.lastResponseBody))
	}
	if !strings.HasSuffix(string(tc.lastResponseBody), "%%EOF\n") {
		return fmt.Errorf("PDF is truncated: it does not end with the EOF marker")
	}
	return nil
}

func (tc *TestContext) responseIsNotAPDF() error {
	if strings.HasPrefix(string(tc.lastResponseBody), "%PDF-") {
		return fmt.Errorf("a refused request still returned a document")
	}
	return nil
}

func (tc *TestContext) statementReads(expected string) error {
	text := tc.statementText()
	if !strings.Contains(text, expected) {
		return fmt.Errorf("statement does not read %q. It reads:\n%s", expected, text)
	}
	return nil
}

func (tc *TestContext) statementNamesTheUserWallet() error {
	return tc.statementReads(tc.walletAddress)
}

func (tc *TestContext) statementNamesTheCurrentMonth() error {
	if tc.statementPeriod.IsZero() {
		return fmt.Errorf("no statement period recorded")
	}
	return tc.statementReads(tc.statementPeriod.Format("January 2006"))
}

func (tc *TestContext) statementListsATransaction() error {
	text := tc.statementText()
	if strings.Contains(text, "No activity in this period") {
		return fmt.Errorf("statement reports no activity, but the account was funded. It reads:\n%s", text)
	}
	if !strings.Contains(text, "transaction(s) in period") {
		return fmt.Errorf("statement does not summarise its transactions. It reads:\n%s", text)
	}
	return nil
}

func (tc *TestContext) statementReportsAmount(amount string) error {
	return tc.statementReads(amount)
}

// ---- transaction setup ----

// userMadeExternalDeposit creates a settled external deposit and records its id.
func (tc *TestContext) userMadeExternalDeposit(amount float64, currency string) error {
	body := map[string]interface{}{
		"user_id":           tc.userID,
		"receiving_address": tc.walletAddress,
		"amount":            amount,
		"currency":          currency,
		"type":              1,
		"deposit_type":      "external",
	}
	return tc.createTransactionAndRecordID(body)
}

// userMadeHostedTransfer creates a hosted transfer, which has no transfer in or
// out to confirm.
func (tc *TestContext) userMadeHostedTransfer(amount float64, currency string) error {
	body := map[string]interface{}{
		"user_id":           tc.userID,
		"sending_address":   tc.walletAddress,
		"receiving_address": "rSomewhereElse",
		"amount":            amount,
		"currency":          currency,
		"type":              2,
		"deposit_type":      "hosted",
	}
	return tc.createTransactionAndRecordID(body)
}

func (tc *TestContext) createTransactionAndRecordID(body map[string]interface{}) error {
	if _, err := tc.request(http.MethodPost, "/core/v1/transactions", body, map[string]string{
		"x-gatehub-managed-user-uuid": tc.userID,
	}); err != nil {
		return err
	}
	if tc.lastResponse.StatusCode != http.StatusCreated {
		return fmt.Errorf("transaction creation failed with status %d: %s",
			tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}

	var created struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &created); err != nil {
		return err
	}
	if created.UUID == "" {
		return fmt.Errorf("transaction response carried no uuid: %s", string(tc.lastResponseBody))
	}
	tc.transactionID = created.UUID
	return nil
}
