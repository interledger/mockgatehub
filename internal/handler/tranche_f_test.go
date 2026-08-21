package handler

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mockgatehub/internal/config"
	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- F1: config wiring ----------

func TestNewHandlerWithConfig_UsesSuppliedConfig(t *testing.T) {
	store := storage.NewMemoryStorage()
	cfg := &config.Config{PublicBaseURL: "https://mock.example.com", CardDataTokenSecret: "s3cret"}

	h := NewHandlerWithConfig(cfg, store, webhook.NewManager("", "", nil, nil, ""))

	assert.Same(t, cfg, h.config, "handler must use the config it was given")
}

func TestNewHandlerWithConfig_NilConfigFallsBackToEnvironment(t *testing.T) {
	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "https://from-env.example.com")
	store := storage.NewMemoryStorage()

	h := NewHandlerWithConfig(nil, store, webhook.NewManager("", "", nil, nil, ""))

	require.NotNil(t, h.config)
	assert.Equal(t, "https://from-env.example.com", h.config.PublicBaseURL)
}

// ---------- F2: sendJSON must not report success for an unmarshalable body ----------

// unmarshalable is a type json.Marshal always rejects.
type unmarshalable struct{}

func (unmarshalable) MarshalJSON() ([]byte, error) {
	return nil, assert.AnError
}

func TestSendJSON_MarshalFailureReportsServerError(t *testing.T) {
	h, _ := newSeededHandler(t)
	rr := httptest.NewRecorder()

	// The caller asked for 200; the body cannot be encoded. Reporting 200 with
	// an error payload would tell the client the request succeeded.
	h.sendJSON(rr, http.StatusOK, unmarshalable{})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"error":"internal server error"}`, rr.Body.String())
}

func TestSendJSON_PassesThroughRequestedStatus(t *testing.T) {
	h, _ := newSeededHandler(t)
	rr := httptest.NewRecorder()

	h.sendJSON(rr, http.StatusCreated, map[string]string{"ok": "yes"})

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"ok":"yes"}`, rr.Body.String())
}

// ---------- F3: malformed iframe amounts must be rejected, not silently zeroed ----------

// iframeBearer registers an iframe token for the seeded user and returns it.
func iframeBearer(t *testing.T, h *Handler, userID string) string {
	t.Helper()
	token := "iframe-token-for-" + userID
	h.tokenToUser.Store(token, userID)
	return token
}

// postIframeDeposit drives the iframe deposit completion route, which is the
// path that actually parses the amount and moves the balance.
func postIframeDeposit(t *testing.T, h *Handler, bearer, amount string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"amount": amount, "currency": "EUR"})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/transaction/complete?paymentType=deposit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	rr := httptest.NewRecorder()
	h.TransactionCompleteHandler(rr, req)
	return rr
}

func TestIframeDeposit_RejectsMalformedAmount(t *testing.T) {
	h, store := newSeededHandler(t)
	bearer := iframeBearer(t, h, consts.TestUser1ID)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	for _, amount := range []string{"not-a-number", "12,50", "1.2.3", "€10"} {
		rr := postIframeDeposit(t, h, bearer, amount)

		assert.Equal(t, http.StatusBadRequest, rr.Code,
			"amount %q must be refused rather than treated as 0.00", amount)

		after, err := store.GetBalance(consts.TestUser1ID, "EUR")
		require.NoError(t, err)
		assert.Equal(t, before, after, "a rejected deposit of %q must not move the balance", amount)
	}
}

func TestIframeDeposit_CreditsBalanceForWellFormedAmount(t *testing.T) {
	h, store := newSeededHandler(t)
	bearer := iframeBearer(t, h, consts.TestUser1ID)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := postIframeDeposit(t, h, bearer, "25.50")
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, before+25.50, after, 0.001, "a successful deposit must credit the balance")
}

// ---------- F5: hosted transfer direction ----------

// createWalletAt gives the user a wallet at a specific address, which the
// hosted-transfer direction tests need to control precisely.
func createWalletAt(t *testing.T, store *storage.MemoryStorage, userID, address string) {
	t.Helper()
	require.NoError(t, store.CreateWallet(&models.Wallet{
		Address: address,
		UserID:  userID,
		Name:    "Test Wallet",
		Type:    consts.WalletTypeStandard,
		Network: consts.NetworkXRPLedger,
	}))
}

func postCoreTransaction(t *testing.T, h *Handler, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/core/v1/transactions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.CreateTransaction(rr, req)
	return rr
}

func TestHostedTransfer_SendingFromOwnWalletDebitsTheUser(t *testing.T) {
	h, store := newSeededHandler(t)
	createWalletAt(t, store, consts.TestUser1ID, "rSenderWallet")

	require.NoError(t, store.AddBalance(consts.TestUser1ID, "EUR", 500.00))
	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := postCoreTransaction(t, h, map[string]interface{}{
		"user_id":           consts.TestUser1ID,
		"sending_address":   "rSenderWallet",
		"receiving_address": "rSomeoneElse",
		"amount":            200.00,
		"currency":          "EUR",
		"type":              consts.TransactionTypeHosted,
		"deposit_type":      consts.DepositTypeHosted,
	})
	require.Equal(t, http.StatusCreated, rr.Code, "body: %s", rr.Body.String())

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, before-200.00, after, 0.001,
		"money leaving the user's own wallet must reduce their balance")
}

func TestHostedTransfer_ReceivingIntoOwnWalletCreditsTheUser(t *testing.T) {
	h, store := newSeededHandler(t)
	createWalletAt(t, store, consts.TestUser1ID, "rReceiverWallet")

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := postCoreTransaction(t, h, map[string]interface{}{
		"user_id":           consts.TestUser1ID,
		"sending_address":   "rExternalSettlement",
		"receiving_address": "rReceiverWallet",
		"amount":            200.00,
		"currency":          "EUR",
		"type":              consts.TransactionTypeHosted,
		"deposit_type":      consts.DepositTypeHosted,
	})
	require.Equal(t, http.StatusCreated, rr.Code, "body: %s", rr.Body.String())

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, before+200.00, after, 0.001,
		"money arriving from elsewhere must increase the balance")
}

func TestHostedTransfer_SendingAddressOwnedByAnotherUserStillCredits(t *testing.T) {
	// The sending wallet exists but belongs to user 2, so from user 1's point of
	// view this is incoming money and must be a credit.
	h, store := newSeededHandler(t)
	createWalletAt(t, store, consts.TestUser2ID, "rOtherUsersWallet")
	createWalletAt(t, store, consts.TestUser1ID, "rUser1Wallet")

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := postCoreTransaction(t, h, map[string]interface{}{
		"user_id":           consts.TestUser1ID,
		"sending_address":   "rOtherUsersWallet",
		"receiving_address": "rUser1Wallet",
		"amount":            75.00,
		"currency":          "EUR",
		"type":              consts.TransactionTypeHosted,
		"deposit_type":      consts.DepositTypeHosted,
	})
	require.Equal(t, http.StatusCreated, rr.Code, "body: %s", rr.Body.String())

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, before+75.00, after, 0.001)
}

func TestExternalDeposit_IgnoresSendingAddressAndCredits(t *testing.T) {
	// An external deposit is always incoming, even if the caller happens to
	// supply a sending_address that resolves to the user's own wallet.
	h, store := newSeededHandler(t)
	createWalletAt(t, store, consts.TestUser1ID, "rUser1Wallet")

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := postCoreTransaction(t, h, map[string]interface{}{
		"user_id":           consts.TestUser1ID,
		"sending_address":   "rUser1Wallet",
		"receiving_address": "rUser1Wallet",
		"amount":            50.00,
		"currency":          "EUR",
		"type":              consts.TransactionTypeDeposit,
		"deposit_type":      consts.DepositTypeExternal,
	})
	require.Equal(t, http.StatusCreated, rr.Code, "body: %s", rr.Body.String())

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Greater(t, after, before, "an external deposit must credit regardless of sending_address")
}

func TestCreateTransaction_EchoesSendingAddress(t *testing.T) {
	h, store := newSeededHandler(t)
	createWalletAt(t, store, consts.TestUser1ID, "rUser1Wallet")

	rr := postCoreTransaction(t, h, map[string]interface{}{
		"user_id":           consts.TestUser1ID,
		"sending_address":   "rSomeSender",
		"receiving_address": "rUser1Wallet",
		"amount":            10.00,
		"currency":          "EUR",
		"type":              consts.TransactionTypeHosted,
		"deposit_type":      consts.DepositTypeHosted,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	var tx map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &tx))
	assert.Equal(t, "rSomeSender", tx["sending_address"],
		"callers need the sending side echoed back to reconcile a transfer")
}

// ---------- F6: helpers ----------

func TestFormatAmount(t *testing.T) {
	cases := map[float64]string{
		0:      "0.00",
		1:      "1.00",
		10.5:   "10.50",
		99.999: "100.00",
		-3.2:   "-3.20",
	}
	for in, want := range cases {
		assert.Equal(t, want, formatAmount(in), "formatAmount(%v)", in)
	}
}

func TestFormatAmount_AlwaysTwoDecimals(t *testing.T) {
	// GateHub renders monetary values as fixed two-decimal strings; consumers
	// parse them, so a bare "10" or "10.5" would be a contract change.
	for _, v := range []float64{0, 0.1, 7, 99.999, math.Pi} {
		out := formatAmount(v)
		dot := strings.IndexByte(out, '.')
		require.NotEqual(t, -1, dot, "formatAmount(%v)=%q has no decimal point", v, out)
		assert.Len(t, out[dot+1:], 2, "formatAmount(%v)=%q", v, out)
	}
}

func TestTokenPrefix_TruncatesLongTokens(t *testing.T) {
	long := strings.Repeat("a", 64)
	assert.Equal(t, strings.Repeat("a", 20), tokenPrefix(long),
		"a log line must not carry a usable credential")
}

func TestTokenPrefix_SafeForShortAndEmptyTokens(t *testing.T) {
	// Hand-rolled slicing at the old call sites panicked here.
	assert.Equal(t, "", tokenPrefix(""))
	assert.Equal(t, "short", tokenPrefix("short"))
	assert.Equal(t, strings.Repeat("b", 20), tokenPrefix(strings.Repeat("b", 20)))
}

func TestResolveWalletParam_PrefersWalletIDOverLegacyAddress(t *testing.T) {
	// Both parameter names are in use across routes; walletID wins when present.
	req := httptest.NewRequest("GET", "/core/v1/wallets/x/balances", nil)
	req = withChiParams(req, map[string]string{"walletID": "rNew", "address": "rLegacy"})
	assert.Equal(t, "rNew", resolveWalletParam(req))

	req = httptest.NewRequest("GET", "/wallets/x/balances", nil)
	req = withChiParams(req, map[string]string{"address": "rLegacy"})
	assert.Equal(t, "rLegacy", resolveWalletParam(req))

	req = httptest.NewRequest("GET", "/wallets/x/balances", nil)
	req = withChiParams(req, map[string]string{})
	assert.Equal(t, "", resolveWalletParam(req))
}
