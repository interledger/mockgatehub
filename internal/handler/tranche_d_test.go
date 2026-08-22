package handler

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pdfTextPattern = regexp.MustCompile(`\(((?:[^()\\]|\\.)*)\) Tj`)

// statementText returns what a reader opening the returned PDF would see.
func statementText(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	matches := pdfTextPattern.FindAllStringSubmatch(rr.Body.String(), -1)
	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		lines = append(lines, strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`).Replace(m[1]))
	}
	return strings.Join(lines, "\n")
}

func assertIsPDFAttachment(t *testing.T, rr *httptest.ResponseRecorder, filename string) {
	t.Helper()
	assert.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Header().Get("Content-Disposition"), filename,
		"the browser needs a filename to save it under")
	assert.True(t, strings.HasPrefix(rr.Body.String(), "%PDF-"), "body must be a PDF")
}

func getStatement(t *testing.T, h *Handler, handler http.HandlerFunc, path string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = withChiParams(req, params)
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// seedWalletAndUser gives user 1 a named profile and a wallet.
func seedWalletAndUser(t *testing.T, store *storage.MemoryStorage) string {
	t.Helper()
	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	user.FirstName = "Ada"
	user.LastName = "Lovelace"
	require.NoError(t, store.UpdateUser(user))

	address := "rStatementWallet"
	require.NoError(t, store.CreateWallet(&models.Wallet{
		Address: address,
		UserID:  consts.TestUser1ID,
		Name:    "Primary EUR",
		Type:    consts.WalletTypeStandard,
		Network: consts.NetworkXRPLedger,
	}))
	return address
}

// ---------- account confirmation ----------

func TestGetAccountConfirmation_NamesTheAccountItConfirms(t *testing.T) {
	h, store := newSeededHandler(t)
	address := seedWalletAndUser(t, store)

	rr := getStatement(t, h, h.GetAccountConfirmation,
		"/statement/v1/statements/account-confirmation/"+address,
		map[string]string{"walletAddress": address})

	assertIsPDFAttachment(t, rr, "account-confirmation.pdf")

	// A confirmation that does not say which account it confirms is useless.
	text := statementText(t, rr)
	assert.Contains(t, text, "Account Confirmation")
	assert.Contains(t, text, address)
	assert.Contains(t, text, "Ada Lovelace")
	assert.Contains(t, text, "Primary EUR")
}

func TestGetAccountConfirmation_StillIssuedForAnUnknownWallet(t *testing.T) {
	// The mock should not 404 a wallet a consumer has not registered here;
	// it says the holder is unknown instead.
	h, _ := newSeededHandler(t)

	rr := getStatement(t, h, h.GetAccountConfirmation,
		"/statement/v1/statements/account-confirmation/rUnknown",
		map[string]string{"walletAddress": "rUnknown"})

	assertIsPDFAttachment(t, rr, "account-confirmation.pdf")
	text := statementText(t, rr)
	assert.Contains(t, text, "rUnknown")
	assert.Contains(t, text, "not on record")
}

func TestGetAccountConfirmation_RequiresAWalletAddress(t *testing.T) {
	h, _ := newSeededHandler(t)

	rr := getStatement(t, h, h.GetAccountConfirmation,
		"/statement/v1/statements/account-confirmation/", map[string]string{})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ---------- account statement ----------

func TestGetAccountStatement_ReportsThePeriodAndItsActivity(t *testing.T) {
	h, store := newSeededHandler(t)
	address := seedWalletAndUser(t, store)

	// Two transactions in the requested month and one outside it.
	inPeriod := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	for _, tx := range []*models.Transaction{
		{ID: "tx-in-1", UserID: consts.TestUser1ID, Amount: "100.00", Currency: "EUR",
			DepositType: consts.DepositTypeExternal, CreatedAt: inPeriod},
		{ID: "tx-in-2", UserID: consts.TestUser1ID, Amount: "25.50", Currency: "EUR",
			DepositType: consts.DepositTypeWithdrawal, CreatedAt: inPeriod.AddDate(0, 0, 3)},
		{ID: "tx-out", UserID: consts.TestUser1ID, Amount: "999.00", Currency: "EUR",
			DepositType: consts.DepositTypeExternal, CreatedAt: inPeriod.AddDate(0, -1, 0)},
	} {
		require.NoError(t, store.CreateTransaction(tx))
	}

	rr := getStatement(t, h, h.GetAccountStatement,
		"/statement/v1/statements/account-statement/"+address+"/2026/3",
		map[string]string{"walletAddress": address, "year": "2026", "month": "3"})

	assertIsPDFAttachment(t, rr, "account-statement.pdf")
	text := statementText(t, rr)

	assert.Contains(t, text, "March 2026", "the statement must name its period")
	assert.Contains(t, text, "tx-in-1")
	assert.Contains(t, text, "tx-in-2")
	assert.NotContains(t, text, "tx-out", "a transaction outside the period must not appear")
	assert.Contains(t, text, "2 transaction(s) in period")
}

func TestGetAccountStatement_SaysSoWhenThereWasNoActivity(t *testing.T) {
	h, store := newSeededHandler(t)
	address := seedWalletAndUser(t, store)

	rr := getStatement(t, h, h.GetAccountStatement,
		"/statement/v1/statements/account-statement/"+address+"/2026/7",
		map[string]string{"walletAddress": address, "year": "2026", "month": "7"})

	assertIsPDFAttachment(t, rr, "account-statement.pdf")
	text := statementText(t, rr)
	assert.Contains(t, text, "July 2026")
	assert.Contains(t, text, "No activity in this period")
}

func TestGetAccountStatement_RejectsANonsensicalPeriod(t *testing.T) {
	// Rendering a statement for month 13 would produce a document that looks
	// authoritative and is meaningless.
	h, store := newSeededHandler(t)
	address := seedWalletAndUser(t, store)

	cases := map[string]map[string]string{
		"month above range": {"walletAddress": address, "year": "2026", "month": "13"},
		"month zero":        {"walletAddress": address, "year": "2026", "month": "0"},
		"month not numeric": {"walletAddress": address, "year": "2026", "month": "March"},
		"year not numeric":  {"walletAddress": address, "year": "twenty", "month": "3"},
		"year implausible":  {"walletAddress": address, "year": "123", "month": "3"},
		"month missing":     {"walletAddress": address, "year": "2026"},
		"year missing":      {"walletAddress": address, "month": "3"},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			rr := getStatement(t, h, h.GetAccountStatement,
				"/statement/v1/statements/account-statement/x/y/z", params)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.NotContains(t, rr.Body.String(), "%PDF-", "no document for an invalid period")
		})
	}
}

func TestParseStatementPeriod_AcceptsTheBoundaries(t *testing.T) {
	for _, month := range []string{"1", "12"} {
		_, m, err := parseStatementPeriod("2026", month)
		require.NoError(t, err, "month %s", month)
		assert.Contains(t, []int{1, 12}, m)
	}

	year, _, err := parseStatementPeriod("1970", "1")
	require.NoError(t, err)
	assert.Equal(t, 1970, year)
}

// ---------- transfer confirmation ----------

func TestGetTransferConfirmation_ReportsTheTransfersFigures(t *testing.T) {
	h, store := newSeededHandler(t)

	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID:               "tx-confirm-1",
		UserID:           consts.TestUser1ID,
		Amount:           "150.00",
		Fee:              "1.50",
		TotalAmount:      "151.50",
		Currency:         "EUR",
		SendingAddress:   "rSender",
		ReceivingAddress: "rReceiver",
		Type:             consts.TransactionTypeDeposit,
		DepositType:      consts.DepositTypeExternal,
		Status:           consts.TransactionStatusCompleted,
	}))

	rr := getStatement(t, h, h.GetTransferConfirmation,
		"/statement/v1/statements/transfer-confirmation/tx-confirm-1",
		map[string]string{"transactionUUID": "tx-confirm-1"})

	assertIsPDFAttachment(t, rr, "transfer-confirmation.pdf")
	text := statementText(t, rr)

	// A confirmation is only useful if it states the actual figures.
	assert.Contains(t, text, "tx-confirm-1")
	assert.Contains(t, text, "150.00 EUR")
	assert.Contains(t, text, "1.50 EUR")
	assert.Contains(t, text, "151.50 EUR")
	assert.Contains(t, text, "rSender")
	assert.Contains(t, text, "rReceiver")
	assert.Contains(t, text, "completed")
}

func TestGetTransferConfirmation_CoversWithdrawalsToo(t *testing.T) {
	h, store := newSeededHandler(t)
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-withdrawal", UserID: consts.TestUser1ID, Amount: "40.00", Currency: "EUR",
		Type: consts.TransactionTypeWithdrawal, DepositType: consts.DepositTypeWithdrawal,
		Status: consts.TransactionStatusPending,
	}))

	rr := getStatement(t, h, h.GetTransferConfirmation,
		"/statement/v1/statements/transfer-confirmation/tx-withdrawal",
		map[string]string{"transactionUUID": "tx-withdrawal"})

	assertIsPDFAttachment(t, rr, "transfer-confirmation.pdf")
	assert.Contains(t, statementText(t, rr), "pending")
}

func TestGetTransferConfirmation_RefusesATransactionWithNoTransfer(t *testing.T) {
	// A hosted transfer between internal wallets is not a transfer in or out,
	// so there is nothing to confirm.
	h, store := newSeededHandler(t)
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-hosted", UserID: consts.TestUser1ID, Amount: "10.00", Currency: "EUR",
		Type: consts.TransactionTypeHosted, DepositType: consts.DepositTypeHosted,
	}))

	rr := getStatement(t, h, h.GetTransferConfirmation,
		"/statement/v1/statements/transfer-confirmation/tx-hosted",
		map[string]string{"transactionUUID": "tx-hosted"})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.NotContains(t, rr.Body.String(), "%PDF-")
}

func TestGetTransferConfirmation_UnknownTransactionIsNotFound(t *testing.T) {
	h, _ := newSeededHandler(t)

	rr := getStatement(t, h, h.GetTransferConfirmation,
		"/statement/v1/statements/transfer-confirmation/nope",
		map[string]string{"transactionUUID": "nope"})

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetTransferConfirmation_RequiresATransactionID(t *testing.T) {
	h, _ := newSeededHandler(t)
	rr := getStatement(t, h, h.GetTransferConfirmation,
		"/statement/v1/statements/transfer-confirmation/", map[string]string{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ---------- distinctness ----------

func TestStatements_AreDistinguishableFromEachOther(t *testing.T) {
	// The fork returned identical bytes from all three endpoints, differing
	// only by filename, so a caller could not tell which it had received.
	h, store := newSeededHandler(t)
	address := seedWalletAndUser(t, store)
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-distinct", UserID: consts.TestUser1ID, Amount: "5.00", Currency: "EUR",
		Type: consts.TransactionTypeDeposit, DepositType: consts.DepositTypeExternal,
	}))

	confirmation := getStatement(t, h, h.GetAccountConfirmation,
		"/x", map[string]string{"walletAddress": address})
	statement := getStatement(t, h, h.GetAccountStatement,
		"/x", map[string]string{"walletAddress": address, "year": "2026", "month": "3"})
	transfer := getStatement(t, h, h.GetTransferConfirmation,
		"/x", map[string]string{"transactionUUID": "tx-distinct"})

	assert.Contains(t, statementText(t, confirmation), "Account Confirmation")
	assert.Contains(t, statementText(t, statement), "Account Statement")
	assert.Contains(t, statementText(t, transfer), "Transfer Confirmation")

	assert.NotEqual(t, confirmation.Body.String(), statement.Body.String())
	assert.NotEqual(t, statement.Body.String(), transfer.Body.String())
}

func TestGetAccountStatement_ContentLengthMatchesTheBody(t *testing.T) {
	// A Content-Length that disagrees with the body truncates the download or
	// leaves the client waiting for bytes that never arrive.
	h, store := newSeededHandler(t)
	address := seedWalletAndUser(t, store)

	rr := getStatement(t, h, h.GetAccountStatement,
		"/x", map[string]string{"walletAddress": address, "year": "2026", "month": "3"})
	require.Equal(t, http.StatusOK, rr.Code)

	declared, err := strconv.Atoi(rr.Header().Get("Content-Length"))
	require.NoError(t, err, "Content-Length must be a number")
	assert.Equal(t, rr.Body.Len(), declared)
}
