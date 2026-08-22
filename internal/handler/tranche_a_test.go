package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRSAKeys returns a base64 SPKI public key and its private key, matching
// the shape a caller sends (WebCrypto 'spki' export, base64-encoded).
func newTestRSAKeys(t *testing.T) (publicKeyB64 string, private *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	return base64.StdEncoding.EncodeToString(der), key
}

// decryptCypher opens a base64 PKCS#1 v1.5 ciphertext, the way a consumer does.
func decryptCypher(t *testing.T, private *rsa.PrivateKey, cypher string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(cypher)
	require.NoError(t, err)
	plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, private, raw)
	require.NoError(t, err, "cypher must be decryptable with the key supplied at token time")
	return plaintext
}

// ---------- A1: the scenario catalogue ----------

func TestCardScenarioCatalogue_LoadsEveryDocumentedScenario(t *testing.T) {
	scenarios := listCardScenarios()

	// The catalogue is the point of the feature; a silently truncated load
	// would leave consumers untested against most card situations.
	assert.Len(t, scenarios, 17)

	byKey := map[string]CardTxScenario{}
	for _, s := range scenarios {
		byKey[s.Key] = s
	}

	// Spot-check one from each group, including the two the source file groups
	// differently (declines have no classification).
	for _, want := range []string{
		"withdrawal.authorization.purchase-transaction",
		"withdrawal.authorization.purchase-transaction-with-fx",
		"withdrawal.authorization.atm-withdrawal",
		"withdrawal.authorization.preauthorization-incremental-with-reftransactionid",
		"deposit.authorization.transfer-to-account-mastercard-send",
		"deposit.reversal.purchase-transaction",
		"none.insufficient-balance",
		"none.wrong-pin",
	} {
		assert.Contains(t, byKey, want)
	}
}

func TestCardScenarioCatalogue_KeysAreUniqueAndStable(t *testing.T) {
	// "Purchase Transaction" exists as both an authorization and a reversal, so
	// the case name alone cannot identify a scenario.
	first := listCardScenarios()
	second := listCardScenarios()
	require.Equal(t, len(first), len(second))

	seen := map[string]bool{}
	for i, s := range first {
		assert.False(t, seen[s.Key], "duplicate key %q", s.Key)
		seen[s.Key] = true
		assert.Equal(t, s.Key, second[i].Key, "ordering must be stable across calls")
	}
}

func TestCardScenarioCatalogue_PayloadsUseTheConsumerFieldNames(t *testing.T) {
	// The upstream fixtures spell this "MastercardConversion", which no
	// consumer reads. Every payload must use the lower-case name.
	for _, s := range listCardScenarios() {
		var fields map[string]interface{}
		require.NoError(t, json.Unmarshal(s.Payload, &fields), "scenario %s", s.Key)

		assert.NotContains(t, fields, "MastercardConversion",
			"scenario %s still uses the capitalised key", s.Key)
		assert.Contains(t, fields, "mastercardConversion",
			"scenario %s is missing mastercardConversion", s.Key)
	}
}

func TestCardScenarioCatalogue_FXScenarioCarriesAConversionRate(t *testing.T) {
	scenario, ok := findCardScenario("withdrawal.authorization.purchase-transaction-with-fx")
	require.True(t, ok)

	var tx models.CardTransaction
	require.NoError(t, json.Unmarshal(scenario.Payload, &tx))

	// A converted transaction without a rate is useless for testing FX display.
	require.NotNil(t, tx.MastercardConversion)
	require.NotNil(t, tx.MastercardConversion.ConvRate)
	assert.NotEmpty(t, *tx.MastercardConversion.ConvRate)
	assert.True(t, tx.IsTrxAmountConverted)
}

func TestCardScenarioCatalogue_DeclineScenariosCarryAFailureCode(t *testing.T) {
	// The decline reason arrives in responseCode for card-level refusals and in
	// ghResponseCode when GateHub itself refuses. A consumer deciding whether a
	// transaction was approved has to see a non-OK code in one of them.
	declines := map[string]string{
		"none.insufficient-balance":       "Insufficient Balance",
		"none.incorrect-cvv":              "Wrong CVV",
		"none.wrong-pin":                  "Wrong PIN",
		"none.wrong-card-expiration-date": "Wrong Card Expiration Date",
		"none.locked-frozen-card":         "Frozen Card",
	}

	for key, wantDescription := range declines {
		scenario, ok := findCardScenario(key)
		require.True(t, ok, "missing decline scenario %s", key)

		var tx models.CardTransaction
		require.NoError(t, json.Unmarshal(scenario.Payload, &tx))

		responseCode := ""
		if tx.ResponseCode != nil {
			responseCode = *tx.ResponseCode
		}
		assert.True(t, tx.GHResponseCode != "OK" || responseCode != "OK",
			"decline scenario %s reports OK in both ghResponseCode (%q) and responseCode (%q)",
			key, tx.GHResponseCode, responseCode)

		// The description is what a consumer surfaces to a user.
		assert.Equal(t, wantDescription, tx.GHResponseDescription, "scenario %s", key)
	}
}

func TestCardScenarioCatalogue_VerificationInquiryIsNotADecline(t *testing.T) {
	// A zero-value verification is grouped with the "no money moved" cases but
	// is an approval, so it must not be mistaken for a refusal.
	scenario, ok := findCardScenario("none.card-verification-inquiry")
	require.True(t, ok)

	var tx models.CardTransaction
	require.NoError(t, json.Unmarshal(scenario.Payload, &tx))

	assert.Equal(t, "OK", tx.GHResponseCode)
	require.NotNil(t, tx.ResponseCode)
	assert.Equal(t, "OK", *tx.ResponseCode)
	assert.Equal(t, consts.CardTxTypeCardVerificationInquiry, tx.Type)
}

func TestFindCardScenario_IsCaseInsensitiveAndRejectsUnknown(t *testing.T) {
	_, ok := findCardScenario("WITHDRAWAL.AUTHORIZATION.ATM-WITHDRAWAL")
	assert.True(t, ok, "lookup should not depend on caller casing")

	_, ok = findCardScenario("  none.wrong-pin  ")
	assert.True(t, ok, "surrounding whitespace should not matter")

	_, ok = findCardScenario("no.such.scenario")
	assert.False(t, ok)
}

// ---------- A2/A3: simulation ----------

// seedSimulatableCard produces a user with an accepted KYC state and a card.
func simulateRequest(t *testing.T, h *Handler, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/card-transactions/simulate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h.SimulateCardTransaction(w, req)
	return w
}

func TestSimulateCardTransaction_ProducesTheScenarioPayload(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId":   consts.TestUser1ID,
		"cardId":   cardID,
		"scenario": "withdrawal.authorization.atm-withdrawal",
	})
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Scenario      string                 `json:"scenario"`
		Event         string                 `json:"event"`
		TransactionID string                 `json:"transactionId"`
		Transaction   map[string]interface{} `json:"transaction"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "withdrawal.authorization.atm-withdrawal", resp.Scenario)
	assert.Equal(t, consts.WebhookEventCardTransactionAuthorization, resp.Event,
		"the default event must be the one that carries the transaction")
	assert.NotEmpty(t, resp.TransactionID)

	// The ATM scenario's distinguishing fields must survive materialisation.
	assert.EqualValues(t, consts.CardTxTypeATMWithdrawal, resp.Transaction["type"])
	assert.Equal(t, "Authorization", resp.Transaction["transactionClassification"])
}

func TestSimulateCardTransaction_AssignsItsOwnIdentityFields(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	first := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID,
		"scenario": "withdrawal.authorization.purchase-transaction",
	})
	second := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID,
		"scenario": "withdrawal.authorization.purchase-transaction",
	})
	require.Equal(t, http.StatusCreated, first.Code)
	require.Equal(t, http.StatusCreated, second.Code)

	a := simulatedTransaction(t, first)
	b := simulatedTransaction(t, second)

	// The fixture ships a fixed transactionId ("TX_PURCHASE") and id. Reusing
	// them would collide the moment a scenario is run twice.
	assert.NotEqual(t, a["transactionId"], b["transactionId"])
	assert.NotEqual(t, "TX_PURCHASE", a["transactionId"])
	assert.Greater(t, b["id"].(float64), a["id"].(float64),
		"the integer id must increase so consumers can order transactions")

	// cardId must describe the card it was filed against, not the card the
	// fixture happened to be captured from.
	assert.EqualValues(t, numericCardID(cardID), a["cardId"])
	assert.EqualValues(t, a["cardId"], b["cardId"], "the same card keeps the same numeric id")
}

func TestSimulateCardTransaction_OverridesApplyButCannotForgeIdentity(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID,
		"scenario": "withdrawal.authorization.purchase-transaction",
		"overrides": map[string]interface{}{
			"transactionAmount": "42.00",
			"billingAmount":     "42.00",
			"merchantName":      "Test Merchant",
			// An attempt to pin the identity fields must not win.
			"transactionId": "forged-id",
		},
	})
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	tx := simulatedTransaction(t, w)
	assert.Equal(t, "42.00", tx["transactionAmount"])
	assert.Equal(t, "Test Merchant", tx["merchantName"])
	assert.NotEqual(t, "forged-id", tx["transactionId"])
}

func TestSimulateCardTransaction_TransactionIsRetrievableVerbatim(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID,
		"scenario": "withdrawal.authorization.purchase-transaction-with-fx",
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := simulatedTransaction(t, w)
	txID := created["transactionId"].(string)

	// Fetching it back must return the same object, including the fields the
	// typed model does not describe — that is why the raw payload is stored.
	req := httptest.NewRequest(http.MethodGet, "/cards/v1/transactions/"+txID, nil)
	req = cardChiParams(req, map[string]string{"txID": txID})
	got := httptest.NewRecorder()
	h.GetCardTransaction(got, req)

	require.Equal(t, http.StatusOK, got.Code)
	var fetched map[string]interface{}
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &fetched))

	assert.Equal(t, created["mcc"], fetched["mcc"])
	assert.Equal(t, created["merchantStreet"], fetched["merchantStreet"])
	assert.Equal(t, created["spendExchangeRate"], fetched["spendExchangeRate"])
	assert.Equal(t, created["mastercardConversion"], fetched["mastercardConversion"])
}

func TestSimulateCardTransaction_AppearsInTheCardListing(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	for _, scenario := range []string{
		"withdrawal.authorization.purchase-transaction",
		"withdrawal.authorization.atm-withdrawal",
		"none.wrong-pin",
	} {
		w := simulateRequest(t, h, map[string]interface{}{
			"userId": consts.TestUser1ID, "cardId": cardID, "scenario": scenario,
		})
		require.Equal(t, http.StatusCreated, w.Code, "scenario %s: %s", scenario, w.Body.String())
	}

	listed := listCardTransactions(t, h, cardID, "")
	assert.Len(t, listed.Data, 3)
	assert.Equal(t, 3, listed.Pagination.TotalRecords)
}

func TestSimulateCardTransaction_RejectsUnknownScenarioAndSaysWhatIsValid(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID, "scenario": "not-a-scenario",
	})
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp struct {
		ValidScenarios []string `json:"validScenarios"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.ValidScenarios, "the error must name the valid options")
	assert.Contains(t, resp.ValidScenarios, "none.wrong-pin")
}

func TestSimulateCardTransaction_RejectsUnknownUserOrCard(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": "no-such-user", "cardId": cardID,
		"scenario": "none.wrong-pin",
	})
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": "no-such-card",
		"scenario": "none.wrong-pin",
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSimulateCardTransaction_RejectsAnUnsupportedEvent(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID,
		"scenario": "none.wrong-pin", "event": "cards.transaction.invented",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetCardTransactionStatus_MovesTheStoredStatus(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID,
		"scenario": "withdrawal.authorization.preauthorization",
	})
	require.Equal(t, http.StatusCreated, w.Code)
	txID := simulatedTransaction(t, w)["transactionId"].(string)

	b, err := json.Marshal(map[string]string{"status": consts.CardTxStatusCompleted})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/admin/card-transactions/"+txID+"/status", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = cardChiParams(req, map[string]string{"txID": txID})
	statusResp := httptest.NewRecorder()
	h.SetCardTransactionStatus(statusResp, req)
	require.Equal(t, http.StatusOK, statusResp.Code, "body: %s", statusResp.Body.String())

	// The change must be visible on the payload the consumer reads back, not
	// only on the typed record.
	get := httptest.NewRequest(http.MethodGet, "/cards/v1/transactions/"+txID, nil)
	get = cardChiParams(get, map[string]string{"txID": txID})
	got := httptest.NewRecorder()
	h.GetCardTransaction(got, get)

	var fetched map[string]interface{}
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &fetched))
	assert.Equal(t, consts.CardTxStatusCompleted, fetched["txStatus"])
}

func TestSetCardTransactionStatus_RejectsUnknownStatusAndMissingTransaction(t *testing.T) {
	h, _ := setupCardsHandler(t)

	b, _ := json.Marshal(map[string]string{"status": "NOT_A_STATUS"})
	req := httptest.NewRequest(http.MethodPost, "/admin/card-transactions/x/status", bytes.NewReader(b))
	req = cardChiParams(req, map[string]string{"txID": "x"})
	w := httptest.NewRecorder()
	h.SetCardTransactionStatus(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	b, _ = json.Marshal(map[string]string{"status": consts.CardTxStatusCompleted})
	req = httptest.NewRequest(http.MethodPost, "/admin/card-transactions/missing/status", bytes.NewReader(b))
	req = cardChiParams(req, map[string]string{"txID": "missing"})
	w = httptest.NewRecorder()
	h.SetCardTransactionStatus(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListCardTxScenarios_ListsTheCatalogue(t *testing.T) {
	h, _ := setupCardsHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/card-transactions/scenarios", nil)
	w := httptest.NewRecorder()
	h.ListCardTxScenarios(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Count     int `json:"count"`
		Scenarios []struct {
			Key            string `json:"key"`
			Operation      string `json:"operation"`
			Classification string `json:"classification"`
			Case           string `json:"case"`
		} `json:"scenarios"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 17, resp.Count)
	require.Len(t, resp.Scenarios, 17)
	for _, s := range resp.Scenarios {
		assert.NotEmpty(t, s.Key)
		assert.NotEmpty(t, s.Operation)
		assert.NotEmpty(t, s.Case)
	}
}

// ---------- A8: pagination ----------

type cardTxListing struct {
	Data       []map[string]interface{} `json:"data"`
	Pagination struct {
		PageNumber   int `json:"pageNumber"`
		PageSize     int `json:"pageSize"`
		TotalPages   int `json:"totalPages"`
		TotalRecords int `json:"totalRecords"`
	} `json:"pagination"`
}

func listCardTransactions(t *testing.T, h *Handler, cardID, query string) cardTxListing {
	t.Helper()
	url := "/cards/v1/cards/" + cardID + "/transactions"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = cardChiParams(req, map[string]string{"cardID": cardID})

	w := httptest.NewRecorder()
	h.ListCardTransactions(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var listing cardTxListing
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listing))
	return listing
}

func TestListCardTransactions_HonoursPageSizeAndPageNumber(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	// Five transactions, so paging is observable.
	for i := 0; i < 5; i++ {
		w := simulateRequest(t, h, map[string]interface{}{
			"userId": consts.TestUser1ID, "cardId": cardID,
			"scenario":  "withdrawal.authorization.purchase-transaction",
			"overrides": map[string]interface{}{"transactionAmount": fmt.Sprintf("%d.00", i+1)},
		})
		require.Equal(t, http.StatusCreated, w.Code)
	}

	first := listCardTransactions(t, h, cardID, "pageSize=2&pageNumber=1")
	assert.Len(t, first.Data, 2)
	assert.Equal(t, 5, first.Pagination.TotalRecords)
	assert.Equal(t, 3, first.Pagination.TotalPages, "5 records at 2 per page is 3 pages")
	assert.Equal(t, 2, first.Pagination.PageSize)
	assert.Equal(t, 1, first.Pagination.PageNumber)

	third := listCardTransactions(t, h, cardID, "pageSize=2&pageNumber=3")
	assert.Len(t, third.Data, 1, "the last page holds the remainder")

	// Pages must not overlap, or a consumer walking them would double-count.
	assert.NotEqual(t, first.Data[0]["transactionId"], third.Data[0]["transactionId"])
}

func TestListCardTransactions_PastTheEndReturnsAnEmptyPage(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	w := simulateRequest(t, h, map[string]interface{}{
		"userId": consts.TestUser1ID, "cardId": cardID, "scenario": "none.wrong-pin",
	})
	require.Equal(t, http.StatusCreated, w.Code)

	listing := listCardTransactions(t, h, cardID, "pageSize=10&pageNumber=9")
	assert.Empty(t, listing.Data)
	assert.Equal(t, 1, listing.Pagination.TotalRecords)
}

func TestListCardTransactions_EmptyCardStillLooksLikeAValidPage(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	listing := listCardTransactions(t, h, cardID, "")
	assert.Empty(t, listing.Data)
	assert.Equal(t, 0, listing.Pagination.TotalRecords)
	assert.Equal(t, 1, listing.Pagination.TotalPages)
	assert.Equal(t, defaultCardTxPageSize, listing.Pagination.PageSize)
}

func TestListCardTransactions_IgnoresNonsensePaginationAndCapsPageSize(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	listing := listCardTransactions(t, h, cardID, "pageSize=abc&pageNumber=-4")
	assert.Equal(t, 1, listing.Pagination.PageNumber)
	assert.Equal(t, defaultCardTxPageSize, listing.Pagination.PageSize)

	// An unbounded page size would let one request ask for everything.
	listing = listCardTransactions(t, h, cardID, "pageSize=100000")
	assert.Equal(t, maxCardTxPageSize, listing.Pagination.PageSize)
}

// ---------- A6/A7: card data and PIN ----------

// mintToken mints a card token directly, bypassing the HTTP layer.
func mintToken(t *testing.T, h *Handler, tokenType, cardID, publicKey string, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	token, err := generateCardToken(h.config.CardDataTokenSecret, CardTokenClaims{
		TokenType: tokenType,
		CardID:    cardID,
		PublicKey: publicKey,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	})
	require.NoError(t, err)
	return token
}

func getWithBearer(t *testing.T, handle http.HandlerFunc, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handle(w, req)
	return w
}

func cypherFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	cypher, ok := resp["cypher"]
	require.True(t, ok, "response must carry a cypher field, got %s", w.Body.String())
	return cypher
}

func TestGetCardData_ReturnsDetailsEncryptedToTheCaller(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)
	pub, priv := newTestRSAKeys(t)

	token := mintToken(t, h, cardTokenTypeCardData, cardID, pub, cardDataTokenTTL)
	w := getWithBearer(t, h.GetCardData, cardDataPath, token)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var details struct {
		Pan        string `json:"Pan"`
		ExpiryDate string `json:"ExpiryDate"`
		Cvc2       string `json:"Cvc2"`
	}
	require.NoError(t, json.Unmarshal(decryptCypher(t, priv, cypherFrom(t, w)), &details))

	assert.Regexp(t, `^\d{16}$`, details.Pan)
	assert.Regexp(t, `^\d{2}/\d{4}$`, details.ExpiryDate)
	assert.Regexp(t, `^\d{3}$`, details.Cvc2)
}

func TestGetCardData_PanIsStableForACard(t *testing.T) {
	// A consumer showing the card twice must not see two different numbers.
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	read := func() string {
		pub, priv := newTestRSAKeys(t)
		token := mintToken(t, h, cardTokenTypeCardData, cardID, pub, cardDataTokenTTL)
		w := getWithBearer(t, h.GetCardData, cardDataPath, token)
		require.Equal(t, http.StatusOK, w.Code)

		var details struct{ Pan string }
		require.NoError(t, json.Unmarshal(decryptCypher(t, priv, cypherFrom(t, w)), &details))
		return details.Pan
	}

	assert.Equal(t, read(), read())
}

func TestGetCardData_ExpiryMatchesTheStoredCard(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	card, err := store.GetCard(cardID)
	require.NoError(t, err)
	require.NotEmpty(t, card.ExpiryDate, "the card object carries an expiry date")
	expected, err := time.Parse("2006-01-02", card.ExpiryDate)
	require.NoError(t, err)

	pub, priv := newTestRSAKeys(t)
	token := mintToken(t, h, cardTokenTypeCardData, cardID, pub, cardDataTokenTTL)
	w := getWithBearer(t, h.GetCardData, cardDataPath, token)
	require.Equal(t, http.StatusOK, w.Code)

	var details struct{ ExpiryDate string }
	require.NoError(t, json.Unmarshal(decryptCypher(t, priv, cypherFrom(t, w)), &details))

	// The sensitive view must agree with the card the consumer already holds.
	assert.Equal(t, expected.Format("01/2006"), details.ExpiryDate)
}

func TestGetCardData_RejectsBadTokens(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)
	pub, _ := newTestRSAKeys(t)

	otherHandler, _ := setupCardsHandler(t)
	otherHandler.config.CardDataTokenSecret = "a-different-secret"

	cases := map[string]string{
		"no token":            "",
		"not a jwt":           "garbage",
		"signed elsewhere":    mintToken(t, otherHandler, cardTokenTypeCardData, cardID, pub, cardDataTokenTTL),
		"already expired":     mintToken(t, h, cardTokenTypeCardData, cardID, pub, -time.Minute),
		"validity beyond cap": mintToken(t, h, cardTokenTypeCardData, cardID, pub, 24*time.Hour),
		"minted for pin":      mintToken(t, h, cardTokenTypePin, cardID, pub, cardDataTokenTTL),
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			w := getWithBearer(t, h.GetCardData, cardDataPath, token)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.NotContains(t, w.Body.String(), "cypher", "no card data may leak on rejection")
		})
	}
}

func TestGetCardData_RejectsATokenWhoseKeyIsUnusable(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	token := mintToken(t, h, cardTokenTypeCardData, cardID, "not-a-key", cardDataTokenTTL)
	w := getWithBearer(t, h.GetCardData, cardDataPath, token)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "cypher")
}

func TestCardTokenRoundTrip_TamperedTokenIsRejected(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)
	pub, _ := newTestRSAKeys(t)

	token := mintToken(t, h, cardTokenTypeCardData, cardID, pub, cardDataTokenTTL)
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	// Re-encode the claims with a different card id but keep the signature.
	claims := CardTokenClaims{TokenType: cardTokenTypeCardData, CardID: "someone-elses-card",
		PublicKey: pub, IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix()}
	forgedPayload, err := json.Marshal(claims)
	require.NoError(t, err)
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(forgedPayload) + "." + parts[2]

	_, err = parseCardToken(h.config.CardDataTokenSecret, forged)
	assert.Error(t, err, "swapping the claims must invalidate the signature")
}

func TestGetCardPin_ReturnsThePinEncryptedToTheCaller(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)
	pub, priv := newTestRSAKeys(t)

	token := mintToken(t, h, cardTokenTypePin, cardID, pub, cardDataTokenTTL)
	w := getWithBearer(t, h.GetCardPin, cardPinPath, token)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// The PIN is the whole plaintext: consumers decrypt and display it as-is.
	pin := string(decryptCypher(t, priv, cypherFrom(t, w)))
	assert.Regexp(t, `^\d{4}$`, pin)
}

func TestGetCardPin_RejectsACardDataToken(t *testing.T) {
	// A token minted to view the card must not also unlock the PIN.
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)
	pub, _ := newTestRSAKeys(t)

	token := mintToken(t, h, cardTokenTypeCardData, cardID, pub, cardDataTokenTTL)
	w := getWithBearer(t, h.GetCardPin, cardPinPath, token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSetCardPin_StoredPinIsWhatIsReadBack(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	// The caller encrypts the new PIN to this service's published key.
	servicePub := servicePublicKeyFromEndpoint(t, h)
	cypher, err := encryptWithBase64SPKI(servicePub, []byte("4321"))
	require.NoError(t, err)

	changeToken := mintToken(t, h, cardTokenTypePinChange, cardID, "", cardDataTokenTTL)
	w := postCypher(t, h.SetCardPin, cardPinPath, changeToken, cypher)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// Reading it back must yield the PIN that was set, not a derived one.
	pub, priv := newTestRSAKeys(t)
	readToken := mintToken(t, h, cardTokenTypePin, cardID, pub, cardDataTokenTTL)
	read := getWithBearer(t, h.GetCardPin, cardPinPath, readToken)
	require.Equal(t, http.StatusOK, read.Code)

	assert.Equal(t, "4321", string(decryptCypher(t, priv, cypherFrom(t, read))))
}

func TestSetCardPin_RejectsUnopenableOrImplausibleValues(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)
	changeToken := mintToken(t, h, cardTokenTypePinChange, cardID, "", cardDataTokenTTL)

	t.Run("encrypted to the wrong key", func(t *testing.T) {
		strangerPub, _ := newTestRSAKeys(t)
		cypher, err := encryptWithBase64SPKI(strangerPub, []byte("4321"))
		require.NoError(t, err)

		w := postCypher(t, h.SetCardPin, cardPinPath, changeToken, cypher)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("not a pin once decrypted", func(t *testing.T) {
		servicePub := servicePublicKeyFromEndpoint(t, h)
		cypher, err := encryptWithBase64SPKI(servicePub, []byte("hunter2"))
		require.NoError(t, err)

		w := postCypher(t, h.SetCardPin, cardPinPath, changeToken, cypher)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing cypher", func(t *testing.T) {
		w := postCypher(t, h.SetCardPin, cardPinPath, changeToken, "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("read token cannot write", func(t *testing.T) {
		pub, _ := newTestRSAKeys(t)
		readToken := mintToken(t, h, cardTokenTypePin, cardID, pub, cardDataTokenTTL)
		servicePub := servicePublicKeyFromEndpoint(t, h)
		cypher, err := encryptWithBase64SPKI(servicePub, []byte("4321"))
		require.NoError(t, err)

		w := postCypher(t, h.SetCardPin, cardPinPath, readToken, cypher)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetCardPinPublicKey_PublishesAUsableKey(t *testing.T) {
	h, _ := setupCardsHandler(t)

	// Without a published key a caller cannot produce a cypher this service
	// can open, so the change-PIN flow would be unusable.
	publicKey := servicePublicKeyFromEndpoint(t, h)
	cypher, err := encryptWithBase64SPKI(publicKey, []byte("1379"))
	require.NoError(t, err)

	plaintext, err := decryptWithServiceKey(cypher)
	require.NoError(t, err)
	assert.Equal(t, "1379", string(plaintext))
}

func servicePublicKeyFromEndpoint(t *testing.T, h *Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/cards/v1/token/pin/public-key", nil)
	w := httptest.NewRecorder()
	h.GetCardPinPublicKey(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["publicKey"])
	return resp["publicKey"]
}

func postCypher(t *testing.T, handle http.HandlerFunc, path, token, cypher string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]string{}
	if cypher != "" {
		body["cypher"] = cypher
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handle(w, req)
	return w
}

// ---------- A4: the legacy create path ----------

func TestCreateCardTransaction_PopulatesTheIdsConsumersNeed(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	body, err := json.Marshal(map[string]interface{}{
		"cardId": cardID, "amount": "12.34", "currency": "EUR", "type": consts.CardTxTypePurchase,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/cards/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreateCardTransaction(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var tx map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tx))

	// Both were previously left null, and consumers key off them.
	require.NotNil(t, tx["id"], "the integer transaction id must be set")
	assert.Positive(t, tx["id"].(float64))
	assert.EqualValues(t, numericCardID(cardID), tx["cardId"])
}

func TestCreateCardTransaction_AcceptsCardGuidAsAnAlias(t *testing.T) {
	h, store := setupCardsHandler(t)
	_, _, cardID := seedCard(t, store)

	body, err := json.Marshal(map[string]interface{}{
		"cardGuid": cardID, "amount": "5.00", "currency": "EUR", "type": consts.CardTxTypePurchase,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/cards/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreateCardTransaction(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Indexing must work through either spelling, or the transaction would be
	// created but never listed against the card.
	listing := listCardTransactions(t, h, cardID, "")
	assert.Len(t, listing.Data, 1)
}

// simulatedTransaction pulls the transaction object out of a simulate response.
func simulatedTransaction(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp struct {
		Transaction map[string]interface{} `json:"transaction"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Transaction)
	return resp.Transaction
}

// ---------- consumer path aliases ----------

func TestListCards_ServesBothConsumerPaths(t *testing.T) {
	// Consumers list a customer's cards under the customer. The same handler
	// serves both spellings, so they must agree.
	h, store := setupCardsHandler(t)
	customerID, _, cardID := seedCard(t, store)

	byCustomerPath := listCardsVia(t, h, "/cards/v1/customers/"+customerID+"/cards", customerID)
	byCardsPath := listCardsVia(t, h, "/cards/v1/cards/"+customerID, customerID)

	require.NotEmpty(t, byCustomerPath)
	assert.Equal(t, byCardsPath, byCustomerPath, "both paths must return the same cards")
	assert.Equal(t, cardID, byCustomerPath[0]["id"])
}

func listCardsVia(t *testing.T, h *Handler, path, customerID string) []map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = cardChiParams(req, map[string]string{"customerID": customerID})

	w := httptest.NewRecorder()
	h.ListCards(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Data
}

func TestGetCardToken_LinkFollowsTheConfiguredPort(t *testing.T) {
	// The link is handed to a browser to follow. If the default base URL
	// ignored the port the process is listening on, the browser would be sent
	// somewhere nothing is served.
	t.Setenv("MOCKGATEHUB_PUBLIC_BASE_URL", "")
	t.Setenv("MOCKGATEHUB_PORT", "9090")

	store := storage.NewMemoryStorage()
	require.NoError(t, storage.SeedTestUsers(store))
	h := NewHandler(store, webhook.NewManager("", "s", nil, store, ""))

	pub, _ := newTestRSAKeys(t)
	resp := postCardTokenRequest(t, h, cardTokenTypeCardData, "card-123", &pub)

	require.Len(t, resp.Links, 1)
	assert.Equal(t, "http://localhost:9090/cards/v1/token/card-data/data", resp.Links[0].Href)
}
