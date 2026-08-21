package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mockgatehub/internal/config"
	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withdrawalHandler builds a handler with the async-withdrawal switch in a
// known position, plus a funded user with a wallet.
func withdrawalHandler(t *testing.T, async bool) (*Handler, *storage.MemoryStorage, string) {
	t.Helper()
	store := storage.NewMemoryStorage()
	require.NoError(t, storage.SeedTestUsers(store))

	cfg := config.Load()
	cfg.AsyncWithdrawals = async
	wm := webhook.NewManager("", "test-secret", nil, store, "default-org")
	h := NewHandlerWithConfig(cfg, store, wm)

	address := "rWithdrawalWallet"
	require.NoError(t, store.CreateWallet(&models.Wallet{
		Address: address, UserID: consts.TestUser1ID, Name: "Primary",
		Type: consts.WalletTypeStandard, Network: consts.NetworkXRPLedger,
	}))
	require.NoError(t, store.AddBalance(consts.TestUser1ID, "EUR", 1000.00))

	return h, store, address
}

// requestWithdrawal drives the iframe withdrawal completion route.
func requestWithdrawal(t *testing.T, h *Handler, amount string) *httptest.ResponseRecorder {
	t.Helper()
	bearer := iframeBearer(t, h, consts.TestUser1ID)

	body, err := json.Marshal(map[string]string{"amount": amount, "currency": "EUR"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/transaction/complete?paymentType=withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	rr := httptest.NewRecorder()
	h.TransactionCompleteHandler(rr, req)
	return rr
}

func withdrawalIDFrom(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["transaction_id"], "the consumer needs the id to follow the withdrawal")
	return resp["transaction_id"]
}

// ---------- default behaviour: settle immediately ----------

func TestWithdrawal_ByDefaultSettlesImmediately(t *testing.T) {
	// This is the behaviour existing consumers depend on. They handle no
	// withdrawal webhooks, so a withdrawal left pending would never complete.
	h, store, _ := withdrawalHandler(t, false)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := requestWithdrawal(t, h, "200.00")
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	txID := withdrawalIDFrom(t, rr)
	tx, err := store.GetTransaction(txID)
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusCompleted, tx.Status)

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Less(t, after, before, "an immediately settled withdrawal must charge the balance")
}

func TestWithdrawal_ChargesTheAmountPlusFee(t *testing.T) {
	h, store, _ := withdrawalHandler(t, false)
	h.feeConfig.SetWithdrawalFeePercent(10)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := requestWithdrawal(t, h, "100.00")
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, before-110.00, after, 0.001, "the fee is charged on top of the amount")

	tx, err := store.GetTransaction(withdrawalIDFrom(t, rr))
	require.NoError(t, err)
	assert.Equal(t, "100.00", tx.Amount)
	assert.Equal(t, "10.00", tx.Fee)
	assert.Equal(t, "110.00", tx.TotalAmount)
}

func TestWithdrawal_RefusesMoreThanTheBalance(t *testing.T) {
	h, store, _ := withdrawalHandler(t, false)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := requestWithdrawal(t, h, "99999.00")
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a refused withdrawal must not move the balance")
}

func TestWithdrawal_CarriesSettlementDetails(t *testing.T) {
	// A consumer showing a withdrawal needs to say where the money went.
	h, store, _ := withdrawalHandler(t, false)

	rr := requestWithdrawal(t, h, "50.00")
	require.Equal(t, http.StatusOK, rr.Code)

	tx, err := store.GetTransaction(withdrawalIDFrom(t, rr))
	require.NoError(t, err)
	assert.NotEmpty(t, tx.AccountIBAN)
	assert.NotEmpty(t, tx.AccountLegalName)
	assert.NotEmpty(t, tx.Message)
}

// ---------- opt-in behaviour: stay pending ----------

func TestWithdrawal_WhenAsyncStaysPendingAndChargesNothing(t *testing.T) {
	h, store, _ := withdrawalHandler(t, true)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := requestWithdrawal(t, h, "200.00")
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	tx, err := store.GetTransaction(withdrawalIDFrom(t, rr))
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusPending, tx.Status)

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Equal(t, before, after,
		"the balance moves when the withdrawal settles, so a rejection leaves it untouched")
}

func TestWithdrawal_WhenAsyncStillChecksTheBalanceUpFront(t *testing.T) {
	// Accepting a withdrawal the account cannot fund only defers the failure.
	h, _, _ := withdrawalHandler(t, true)

	rr := requestWithdrawal(t, h, "99999.00")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ---------- settlement ----------

func triggerWithdrawalEvent(t *testing.T, h *Handler, txID, event string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"event": event})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/withdrawals/"+txID+"/trigger-event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"txID": txID})

	rr := httptest.NewRecorder()
	h.TriggerWithdrawalEvent(rr, req)
	return rr
}

func TestTriggerWithdrawalEvent_CompletionChargesTheBalance(t *testing.T) {
	h, store, _ := withdrawalHandler(t, true)

	created := requestWithdrawal(t, h, "200.00")
	require.Equal(t, http.StatusOK, created.Code)
	txID := withdrawalIDFrom(t, created)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalCompleted)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	tx, err := store.GetTransaction(txID)
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusCompleted, tx.Status)

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, before-200.00, after, 0.001)
}

func TestTriggerWithdrawalEvent_RejectionLeavesTheBalanceAlone(t *testing.T) {
	h, store, _ := withdrawalHandler(t, true)

	created := requestWithdrawal(t, h, "200.00")
	require.Equal(t, http.StatusOK, created.Code)
	txID := withdrawalIDFrom(t, created)

	before, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	rr := triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalRejected)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	tx, err := store.GetTransaction(txID)
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusFailed, tx.Status)

	after, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a rejected withdrawal must never charge the account")
}

func TestTriggerWithdrawalEvent_CannotSettleTwice(t *testing.T) {
	// Settling twice would charge the account again or contradict an already
	// published outcome.
	h, store, _ := withdrawalHandler(t, true)

	created := requestWithdrawal(t, h, "100.00")
	txID := withdrawalIDFrom(t, created)

	require.Equal(t, http.StatusOK, triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalCompleted).Code)

	afterFirst, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	second := triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalCompleted)
	assert.Equal(t, http.StatusConflict, second.Code)

	afterSecond, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Equal(t, afterFirst, afterSecond, "a repeated settlement must not charge again")
}

func TestTriggerWithdrawalEvent_CannotContradictAnOutcome(t *testing.T) {
	h, store, _ := withdrawalHandler(t, true)

	created := requestWithdrawal(t, h, "100.00")
	txID := withdrawalIDFrom(t, created)

	require.Equal(t, http.StatusOK, triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalRejected).Code)

	// Completing an already-rejected withdrawal would charge for money the
	// consumer has been told was returned.
	second := triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalCompleted)
	assert.Equal(t, http.StatusConflict, second.Code)

	tx, err := store.GetTransaction(txID)
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusFailed, tx.Status)
}

func TestTriggerWithdrawalEvent_RefusesBadInput(t *testing.T) {
	h, store, _ := withdrawalHandler(t, true)

	t.Run("unknown event", func(t *testing.T) {
		created := requestWithdrawal(t, h, "10.00")
		txID := withdrawalIDFrom(t, created)

		rr := triggerWithdrawalEvent(t, h, txID, "core.withdrawal.invented")
		assert.Equal(t, http.StatusBadRequest, rr.Code)

		tx, err := store.GetTransaction(txID)
		require.NoError(t, err)
		assert.Equal(t, consts.TransactionStatusPending, tx.Status, "the withdrawal must be left pending")
	})

	t.Run("unknown transaction", func(t *testing.T) {
		rr := triggerWithdrawalEvent(t, h, "no-such-tx", consts.WebhookEventWithdrawalCompleted)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("not a withdrawal", func(t *testing.T) {
		require.NoError(t, store.CreateTransaction(&models.Transaction{
			ID: "tx-deposit", UserID: consts.TestUser1ID, Amount: "10.00", TotalAmount: "10.00",
			Currency: "EUR", Type: consts.TransactionTypeDeposit,
			DepositType: consts.DepositTypeExternal, Status: consts.TransactionStatusPending,
		}))

		rr := triggerWithdrawalEvent(t, h, "tx-deposit", consts.WebhookEventWithdrawalCompleted)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestTriggerWithdrawalEvent_DoesNotCompleteWhatItCannotCharge(t *testing.T) {
	// If the balance has been spent since the withdrawal was requested, the
	// withdrawal must not be reported as completed.
	h, store, _ := withdrawalHandler(t, true)

	created := requestWithdrawal(t, h, "900.00")
	txID := withdrawalIDFrom(t, created)

	// Drain the account behind the withdrawal's back.
	require.NoError(t, store.DeductBalance(consts.TestUser1ID, "EUR", 950.00))

	rr := triggerWithdrawalEvent(t, h, txID, consts.WebhookEventWithdrawalCompleted)
	assert.Equal(t, http.StatusConflict, rr.Code)

	tx, err := store.GetTransaction(txID)
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusPending, tx.Status,
		"a withdrawal that could not be charged must not be marked completed")
}

// ---------- listing ----------

func listWithdrawals(t *testing.T, h *Handler, userID, status string) (*httptest.ResponseRecorder, []map[string]interface{}) {
	t.Helper()
	path := "/admin/users/" + userID + "/withdrawals"
	if status != "" {
		path += "?status=" + status
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = withChiParams(req, map[string]string{"userID": userID})

	rr := httptest.NewRecorder()
	h.ListWithdrawals(rr, req)

	if rr.Code != http.StatusOK {
		return rr, nil
	}
	var resp struct {
		Withdrawals []map[string]interface{} `json:"withdrawals"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	return rr, resp.Withdrawals
}

func TestListWithdrawals_ReturnsOnlyWithdrawals(t *testing.T) {
	h, store, address := withdrawalHandler(t, true)

	// A deposit must not appear in a withdrawal listing.
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-a-deposit", UserID: consts.TestUser1ID, Amount: "10.00", Currency: "EUR",
		ReceivingAddress: address, Type: consts.TransactionTypeDeposit,
		DepositType: consts.DepositTypeExternal, Status: consts.TransactionStatusCompleted,
	}))
	created := requestWithdrawal(t, h, "20.00")
	txID := withdrawalIDFrom(t, created)

	rr, withdrawals := listWithdrawals(t, h, consts.TestUser1ID, "")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, withdrawals, 1)
	assert.Equal(t, txID, withdrawals[0]["uuid"])
}

func TestListWithdrawals_FiltersByStatus(t *testing.T) {
	h, _, _ := withdrawalHandler(t, true)

	pendingID := withdrawalIDFrom(t, requestWithdrawal(t, h, "10.00"))
	settledID := withdrawalIDFrom(t, requestWithdrawal(t, h, "20.00"))
	require.Equal(t, http.StatusOK, triggerWithdrawalEvent(t, h, settledID, consts.WebhookEventWithdrawalCompleted).Code)

	_, pending := listWithdrawals(t, h, consts.TestUser1ID, "pending")
	require.Len(t, pending, 1)
	assert.Equal(t, pendingID, pending[0]["uuid"])

	_, completed := listWithdrawals(t, h, consts.TestUser1ID, "completed")
	require.Len(t, completed, 1)
	assert.Equal(t, settledID, completed[0]["uuid"])

	_, all := listWithdrawals(t, h, consts.TestUser1ID, "")
	assert.Len(t, all, 2)
}

func TestListWithdrawals_RefusesAnUnknownStatusFilter(t *testing.T) {
	// Silently ignoring the filter would return everything and look like a
	// passing assertion.
	h, _, _ := withdrawalHandler(t, true)

	rr, _ := listWithdrawals(t, h, consts.TestUser1ID, "nearly-done")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestListWithdrawals_EmptyForAUserWithNone(t *testing.T) {
	h, _, _ := withdrawalHandler(t, true)

	rr, withdrawals := listWithdrawals(t, h, consts.TestUser2ID, "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, withdrawals)
	assert.Contains(t, rr.Body.String(), `"withdrawals": []`,
		"an empty listing must be an array, not null")
}

func TestWithdrawalStatusFilter(t *testing.T) {
	none, err := withdrawalStatusFilter("")
	require.NoError(t, err)
	assert.Nil(t, none, "no filter means every status")

	for filter, want := range map[string]int{
		"pending":   consts.TransactionStatusPending,
		"completed": consts.TransactionStatusCompleted,
		"failed":    consts.TransactionStatusFailed,
		"rejected":  consts.TransactionStatusFailed,
	} {
		got, err := withdrawalStatusFilter(filter)
		require.NoError(t, err, filter)
		require.NotNil(t, got)
		assert.Equal(t, want, *got, filter)
	}

	_, err = withdrawalStatusFilter("in-progress")
	assert.Error(t, err)
}
