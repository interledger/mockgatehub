package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- storage the UI depends on ----------

func TestGetAllBalances_ReturnsEveryNonZeroHolding(t *testing.T) {
	store := storage.NewMemoryStorage()
	require.NoError(t, store.AddBalance("u1", "EUR", 10.50))
	require.NoError(t, store.AddBalance("u1", "USD", 3.25))
	require.NoError(t, store.AddBalance("u2", "GBP", 99.00))

	balances, err := store.GetAllBalances("u1")
	require.NoError(t, err)

	// A view showing an account at a glance must not have to probe every
	// currency, nor see another user's money.
	assert.Equal(t, map[string]float64{"EUR": 10.50, "USD": 3.25}, balances)
}

func TestGetAllBalances_OmitsZeroAndUnknownUsers(t *testing.T) {
	store := storage.NewMemoryStorage()
	require.NoError(t, store.AddBalance("u1", "EUR", 10.00))
	require.NoError(t, store.DeductBalance("u1", "EUR", 10.00))

	balances, err := store.GetAllBalances("u1")
	require.NoError(t, err)
	assert.Empty(t, balances, "a zero balance is noise in a listing")

	balances, err = store.GetAllBalances("nobody")
	require.NoError(t, err, "an unknown user is not an error")
	assert.Empty(t, balances)
}

func TestListUsers_ReturnsEveryUserInAStableOrder(t *testing.T) {
	store := storage.NewMemoryStorage()
	require.NoError(t, storage.SeedTestUsers(store))

	first, err := store.ListUsers()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(first), 2)

	second, err := store.ListUsers()
	require.NoError(t, err)

	// Map iteration is random; a listing that reordered between requests would
	// make the UI jump around.
	require.Len(t, second, len(first))
	for i := range first {
		assert.Equal(t, first[i].ID, second[i].ID, "listing order must be stable")
	}
}

// ---------- the views ----------

func uiGet(t *testing.T, handler http.HandlerFunc, path string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if params != nil {
		req = withChiParams(req, params)
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func uiPost(t *testing.T, handler http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestUIDashboard_ShowsUsersAndTheirBalances(t *testing.T) {
	h, store := newSeededHandler(t)
	require.NoError(t, store.AddBalance(consts.TestUser1ID, "EUR", 42.50))

	rr := uiGet(t, h.UIDashboard, "/ui", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "text/html")

	body := rr.Body.String()
	assert.Contains(t, body, consts.TestUser1Email)
	assert.Contains(t, body, "42.50")
	assert.Contains(t, body, "/ui/users/"+consts.TestUser1ID, "each user must link to their detail view")
}

func TestUIUserDetail_ShowsBalancesCardsAndTransactions(t *testing.T) {
	h, store := newSeededHandler(t)
	_, _, cardID := seedCard(t, store)
	require.NoError(t, store.AddBalance(consts.TestUser1ID, "EUR", 12.00))
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-shown", UserID: consts.TestUser1ID, Amount: "7.00", Currency: "EUR",
		DepositType: consts.DepositTypeExternal, Status: consts.TransactionStatusCompleted,
	}))

	rr := uiGet(t, h.UIUserDetail, "/ui/users/"+consts.TestUser1ID,
		map[string]string{"userID": consts.TestUser1ID})
	require.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	assert.Contains(t, body, "12.00")
	assert.Contains(t, body, cardID, "the user's card must be listed")
	assert.Contains(t, body, "tx-shown")
	assert.Contains(t, body, "completed", "the status must be readable, not a raw number")
}

func TestUIUserDetail_UnknownUserIsNotFound(t *testing.T) {
	h, _ := newSeededHandler(t)
	rr := uiGet(t, h.UIUserDetail, "/ui/users/nobody", map[string]string{"userID": "nobody"})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUIUserDetail_OffersSettlementOnlyForPendingWithdrawals(t *testing.T) {
	// Offering a settle button for an already-settled withdrawal would invite a
	// conflict the user cannot understand.
	h, store := newSeededHandler(t)

	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-pending-withdrawal", UserID: consts.TestUser1ID, Amount: "10.00", TotalAmount: "10.00",
		Currency: "EUR", DepositType: consts.DepositTypeWithdrawal, Status: consts.TransactionStatusPending,
	}))
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-settled-withdrawal", UserID: consts.TestUser1ID, Amount: "20.00", TotalAmount: "20.00",
		Currency: "EUR", DepositType: consts.DepositTypeWithdrawal, Status: consts.TransactionStatusCompleted,
	}))
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-pending-deposit", UserID: consts.TestUser1ID, Amount: "30.00", TotalAmount: "30.00",
		Currency: "EUR", DepositType: consts.DepositTypeExternal, Status: consts.TransactionStatusPending,
	}))

	rr := uiGet(t, h.UIUserDetail, "/ui/users/"+consts.TestUser1ID,
		map[string]string{"userID": consts.TestUser1ID})
	require.Equal(t, http.StatusOK, rr.Code)

	// The settle form carries the transaction id in a hidden field.
	body := rr.Body.String()
	assert.Contains(t, body, `value="tx-pending-withdrawal"`)
	assert.NotContains(t, body, `value="tx-settled-withdrawal"`)
	assert.NotContains(t, body, `value="tx-pending-deposit"`,
		"a pending deposit is not a withdrawal awaiting settlement")
}

func TestUIKYCForm_OffersEveryOutcomeAndEveryUser(t *testing.T) {
	h, _ := newSeededHandler(t)

	rr := uiGet(t, h.UIKYCForm, "/ui/actions/kyc", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	for _, outcome := range kycOutcomeOptions() {
		assert.Contains(t, body, `value="`+outcome.Value+`"`, "missing outcome %s", outcome.Value)
	}
	assert.Contains(t, body, consts.TestUser1Email)
	assert.Contains(t, body, consts.TestUser2Email)
}

func TestUICardTxForm_OffersTheWholeCatalogue(t *testing.T) {
	h, _ := newSeededHandler(t)

	rr := uiGet(t, h.UICardTxForm, "/ui/actions/card-transaction", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	// The form is driven by the catalogue, so every scenario must be selectable
	// rather than the user having to hand-write JSON.
	body := rr.Body.String()
	scenarios := listCardScenarios()
	require.NotEmpty(t, scenarios)
	for _, scenario := range scenarios {
		assert.Contains(t, body, `value="`+scenario.Key+`"`, "missing scenario %s", scenario.Key)
	}
}

func TestUICardTxForm_ListsTheSelectedUsersCards(t *testing.T) {
	h, store := newSeededHandler(t)
	_, _, cardID := seedCard(t, store)

	rr := uiGet(t, h.UICardTxForm, "/ui/actions/card-transaction?userID="+consts.TestUser1ID, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), cardID)
}

// ---------- the actions ----------

func redirectTarget(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, rr.Code, "body: %s", rr.Body.String())
	return rr.Header().Get("Location")
}

func TestUIKYCAction_MovesTheStateAndReportsWhatItSent(t *testing.T) {
	h, store := newSeededHandler(t)

	rr := uiPost(t, h.UIKYCAction, "/ui/actions/kyc", url.Values{
		"userID":  {consts.TestUser1ID},
		"outcome": {consts.KYCStateRejected},
		"message": {"not this time"},
	})

	target := redirectTarget(t, rr)
	assert.Contains(t, target, "/ui/users/"+consts.TestUser1ID)
	assert.Contains(t, target, url.QueryEscape(consts.WebhookEventKYCRejected),
		"the user must be told which event was sent")

	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateRejected, user.KYCState)
}

func TestUIKYCAction_DocumentNoticeDoesNotChangeVerificationState(t *testing.T) {
	// A document notice says a document is expiring; it is not a verdict on the
	// user's verification, so their state must be left alone.
	h, store := newSeededHandler(t)

	before, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	stateBefore := before.KYCState

	rr := uiPost(t, h.UIKYCAction, "/ui/actions/kyc", url.Values{
		"userID":  {consts.TestUser1ID},
		"outcome": {consts.WebhookEventDocumentNoticeExpired},
	})

	target := redirectTarget(t, rr)
	assert.Contains(t, target, url.QueryEscape(consts.WebhookEventDocumentNoticeExpired))

	after, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, stateBefore, after.KYCState)
}

func TestUIKYCAction_ReportsBadInputWithoutChangingAnything(t *testing.T) {
	h, store := newSeededHandler(t)
	before, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)

	cases := map[string]url.Values{
		"no user":         {"outcome": {consts.KYCStateAccepted}},
		"no outcome":      {"userID": {consts.TestUser1ID}},
		"unknown user":    {"userID": {"nobody"}, "outcome": {consts.KYCStateAccepted}},
		"unknown outcome": {"userID": {consts.TestUser1ID}, "outcome": {"probably-fine"}},
	}

	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			rr := uiPost(t, h.UIKYCAction, "/ui/actions/kyc", form)

			target := redirectTarget(t, rr)
			assert.Contains(t, target, "error=1", "the failure must be visible to the user")

			after, err := store.GetUser(consts.TestUser1ID)
			require.NoError(t, err)
			assert.Equal(t, before.KYCState, after.KYCState)
		})
	}
}

func TestUICardTxAction_CreatesTheTransactionAndSaysWhatItSent(t *testing.T) {
	h, store := newSeededHandler(t)
	_, _, cardID := seedCard(t, store)

	rr := uiPost(t, h.UICardTxAction, "/ui/actions/card-transaction", url.Values{
		"userID":   {consts.TestUser1ID},
		"cardID":   {cardID},
		"scenario": {"withdrawal.authorization.atm-withdrawal"},
	})

	target := redirectTarget(t, rr)
	assert.Contains(t, target, "/ui/users/"+consts.TestUser1ID)
	assert.Contains(t, target, url.QueryEscape("withdrawal.authorization.atm-withdrawal"))
	assert.Contains(t, target, url.QueryEscape(consts.WebhookEventCardTransactionAuthorization))

	// The transaction must really exist, filed against the card.
	listing := listCardTransactions(t, h, cardID, "")
	require.Len(t, listing.Data, 1)
	assert.EqualValues(t, consts.CardTxTypeATMWithdrawal, listing.Data[0]["type"])
}

func TestUICardTxAction_AppliesAmountAndMerchantOverrides(t *testing.T) {
	h, store := newSeededHandler(t)
	_, _, cardID := seedCard(t, store)

	rr := uiPost(t, h.UICardTxAction, "/ui/actions/card-transaction", url.Values{
		"userID":       {consts.TestUser1ID},
		"cardID":       {cardID},
		"scenario":     {"withdrawal.authorization.purchase-transaction"},
		"amount":       {"42.00"},
		"merchantName": {"Test Merchant"},
	})
	redirectTarget(t, rr)

	listing := listCardTransactions(t, h, cardID, "")
	require.Len(t, listing.Data, 1)
	tx := listing.Data[0]

	assert.Equal(t, "42.00", tx["transactionAmount"])
	// Billing and transaction amounts must move together, or the transaction
	// contradicts itself.
	assert.Equal(t, "42.00", tx["billingAmount"])
	assert.Equal(t, "Test Merchant", tx["merchantName"])
}

func TestUICardTxAction_ReportsBadInput(t *testing.T) {
	h, store := newSeededHandler(t)
	_, _, cardID := seedCard(t, store)

	cases := map[string]url.Values{
		"no user":          {"cardID": {cardID}, "scenario": {"none.wrong-pin"}},
		"no card":          {"userID": {consts.TestUser1ID}, "scenario": {"none.wrong-pin"}},
		"no scenario":      {"userID": {consts.TestUser1ID}, "cardID": {cardID}},
		"unknown scenario": {"userID": {consts.TestUser1ID}, "cardID": {cardID}, "scenario": {"nope"}},
		"unknown card":     {"userID": {consts.TestUser1ID}, "cardID": {"nope"}, "scenario": {"none.wrong-pin"}},
	}

	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			rr := uiPost(t, h.UICardTxAction, "/ui/actions/card-transaction", form)
			assert.Contains(t, redirectTarget(t, rr), "error=1")
		})
	}

	// No transaction should have been recorded by any of the failures.
	assert.Empty(t, listCardTransactions(t, h, cardID, "").Data)
}

func TestUIWithdrawalSettle_SettlesThroughTheSharedPath(t *testing.T) {
	// The UI must not have its own copy of the settlement rules.
	h, store := newSeededHandler(t)
	require.NoError(t, store.AddBalance(consts.TestUser1ID, "EUR", 500.00))
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-ui-withdrawal", UserID: consts.TestUser1ID, Amount: "100.00", TotalAmount: "100.00",
		Currency: "EUR", DepositType: consts.DepositTypeWithdrawal, Status: consts.TransactionStatusPending,
	}))

	rr := uiPost(t, h.UIWithdrawalSettle, "/ui/actions/withdrawal/settle", url.Values{
		"userID": {consts.TestUser1ID},
		"txID":   {"tx-ui-withdrawal"},
		"event":  {consts.WebhookEventWithdrawalCompleted},
	})
	assert.Contains(t, redirectTarget(t, rr), "/ui/users/"+consts.TestUser1ID)

	tx, err := store.GetTransaction("tx-ui-withdrawal")
	require.NoError(t, err)
	assert.Equal(t, consts.TransactionStatusCompleted, tx.Status)

	balance, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.InDelta(t, 400.00, balance, 0.001)
}

func TestUIWithdrawalSettle_SurfacesAConflictRatherThanSettlingTwice(t *testing.T) {
	h, store := newSeededHandler(t)
	require.NoError(t, store.AddBalance(consts.TestUser1ID, "EUR", 500.00))
	require.NoError(t, store.CreateTransaction(&models.Transaction{
		ID: "tx-twice", UserID: consts.TestUser1ID, Amount: "100.00", TotalAmount: "100.00",
		Currency: "EUR", DepositType: consts.DepositTypeWithdrawal, Status: consts.TransactionStatusPending,
	}))

	require.Contains(t, redirectTarget(t, uiPost(t, h.UIWithdrawalSettle, "/ui/actions/withdrawal/settle", url.Values{
		"userID": {consts.TestUser1ID}, "txID": {"tx-twice"},
		"event": {consts.WebhookEventWithdrawalCompleted},
	})), "/ui/users/")

	balanceAfterFirst, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)

	second := uiPost(t, h.UIWithdrawalSettle, "/ui/actions/withdrawal/settle", url.Values{
		"userID": {consts.TestUser1ID}, "txID": {"tx-twice"},
		"event": {consts.WebhookEventWithdrawalCompleted},
	})
	assert.Contains(t, redirectTarget(t, second), "error=1")

	balanceAfterSecond, err := store.GetBalance(consts.TestUser1ID, "EUR")
	require.NoError(t, err)
	assert.Equal(t, balanceAfterFirst, balanceAfterSecond, "the account must not be charged twice")
}

func TestUIWithdrawalSettle_ReportsBadInput(t *testing.T) {
	h, _ := newSeededHandler(t)

	rr := uiPost(t, h.UIWithdrawalSettle, "/ui/actions/withdrawal/settle", url.Values{
		"userID": {consts.TestUser1ID},
	})
	assert.Contains(t, redirectTarget(t, rr), "error=1")

	rr = uiPost(t, h.UIWithdrawalSettle, "/ui/actions/withdrawal/settle", url.Values{
		"userID": {consts.TestUser1ID}, "txID": {"nope"},
		"event": {consts.WebhookEventWithdrawalCompleted},
	})
	assert.Contains(t, redirectTarget(t, rr), "error=1")
}

// ---------- rendering safety ----------

func TestUI_EscapesUserSuppliedText(t *testing.T) {
	// A user's own details are rendered into the page; emitting them raw would
	// let stored data execute as markup.
	h, store := newSeededHandler(t)
	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	user.FirstName = `<script>alert(1)</script>`
	require.NoError(t, store.UpdateUser(user))

	rr := uiGet(t, h.UIUserDetail, "/ui/users/"+consts.TestUser1ID,
		map[string]string{"userID": consts.TestUser1ID})
	require.Equal(t, http.StatusOK, rr.Code)

	assert.NotContains(t, rr.Body.String(), "<script>alert(1)</script>")
	assert.Contains(t, rr.Body.String(), "&lt;script&gt;")
}

func TestUITemplates_AllParse(t *testing.T) {
	// Every page must be renderable; a template error would only show up as a
	// half-written response at runtime.
	for page := range uiPages {
		assert.NotNil(t, uiPageTemplates[page], "page %s has no parsed template", page)
	}
}

func TestUIKYCAction_AssignsARiskLevelWhenTheUserHasNone(t *testing.T) {
	// A user the provider has ruled on always carries a risk level. Leaving it
	// empty produces a state the iframe flow could never produce.
	h, store := newSeededHandler(t)
	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	user.RiskLevel = ""
	require.NoError(t, store.UpdateUser(user))

	rr := uiPost(t, h.UIKYCAction, "/ui/actions/kyc", url.Values{
		"userID":  {consts.TestUser1ID},
		"outcome": {consts.KYCStateAccepted},
	})
	redirectTarget(t, rr)

	updated, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, consts.RiskLevelLow, updated.RiskLevel)
}

func TestUIKYCAction_LeavesAnExistingRiskLevelAlone(t *testing.T) {
	h, store := newSeededHandler(t)
	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	user.RiskLevel = consts.RiskLevelHigh
	require.NoError(t, store.UpdateUser(user))

	rr := uiPost(t, h.UIKYCAction, "/ui/actions/kyc", url.Values{
		"userID":  {consts.TestUser1ID},
		"outcome": {consts.KYCStateAccepted},
	})
	redirectTarget(t, rr)

	updated, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, consts.RiskLevelHigh, updated.RiskLevel,
		"an assessed risk level must not be reset by a verification outcome")
}
