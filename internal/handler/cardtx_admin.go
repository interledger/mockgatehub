package handler

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"
	"mockgatehub/internal/utils"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SimulateCardTransactionRequest selects a catalogue scenario and materialises
// it against a real card.
type SimulateCardTransactionRequest struct {
	// UserID is the managed user the webhook is attributed to.
	UserID string `json:"userId"`
	// CardID is the card UUID the transaction is filed against.
	CardID string `json:"cardId"`
	// Scenario is a catalogue key, e.g. "withdrawal.authorization.atm-withdrawal".
	Scenario string `json:"scenario"`
	// Overrides replaces individual fields of the scenario payload, so a test
	// can pin an amount or a merchant without hand-writing the whole object.
	Overrides map[string]interface{} `json:"overrides,omitempty"`
	// Event selects the webhook to emit. Defaults to
	// cards.transaction.authorization, which is the one that carries the
	// transaction itself.
	Event string `json:"event,omitempty"`
	// Notification supplies the title and body for the notification-shaped
	// cards.transaction.event webhook. Ignored for other events.
	Notification *CardTxNotification `json:"notification,omitempty"`
}

// CardTxNotification is the title/body pair carried by the notification-shaped
// card transaction webhook.
type CardTxNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ListCardTxScenarios returns the card transaction catalogue so a caller can
// discover which situations it can simulate without reading the source.
// GET /admin/card-transactions/scenarios
func (h *Handler) ListCardTxScenarios(w http.ResponseWriter, r *http.Request) {
	scenarios := listCardScenarios()

	// Summaries only: the payloads are large and a caller listing scenarios is
	// choosing between them, not consuming them.
	type summary struct {
		Key            string `json:"key"`
		Operation      string `json:"operation"`
		Classification string `json:"classification"`
		Case           string `json:"case"`
	}
	out := make([]summary, 0, len(scenarios))
	for _, s := range scenarios {
		out = append(out, summary{s.Key, s.Operation, s.Classification, s.Case})
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"scenarios": out,
		"count":     len(out),
	})
}

// SimulateCardTransactionResult describes what a simulation produced.
type SimulateCardTransactionResult struct {
	Scenario      string
	Event         string
	TransactionID string
	// Transaction is the stored payload, verbatim.
	Transaction json.RawMessage
}

// SimulateCardTransaction creates a card transaction from a catalogue scenario
// and emits the corresponding webhook.
// POST /admin/card-transactions/simulate
func (h *Handler) SimulateCardTransaction(w http.ResponseWriter, r *http.Request) {
	var req SimulateCardTransactionRequest
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.simulateCardTransaction(req)
	if err != nil {
		h.sendAPIError(w, err)
		return
	}

	h.sendJSON(w, http.StatusCreated, map[string]interface{}{
		"scenario":      result.Scenario,
		"event":         result.Event,
		"transactionId": result.TransactionID,
		"transaction":   result.Transaction,
	})
}

// simulateCardTransaction is the shared implementation behind both the
// simulation endpoint and the admin UI, so the two cannot drift apart.
func (h *Handler) simulateCardTransaction(req SimulateCardTransactionRequest) (*SimulateCardTransactionResult, error) {
	if req.UserID == "" {
		return nil, badRequest("userId is required")
	}
	if req.CardID == "" {
		return nil, badRequest("cardId is required")
	}
	if req.Scenario == "" {
		return nil, badRequest("scenario is required; GET /admin/card-transactions/scenarios lists the available keys")
	}

	if _, err := h.store.GetUser(req.UserID); err != nil {
		return nil, notFound("user not found")
	}
	card, err := h.store.GetCard(req.CardID)
	if err != nil {
		return nil, notFound("card not found")
	}

	scenario, ok := findCardScenario(req.Scenario)
	if !ok {
		// Name the valid options rather than making the caller guess.
		return nil, badRequest("unknown scenario " + req.Scenario).
			withDetails(map[string]interface{}{"validScenarios": scenarioKeys()})
	}

	event := req.Event
	if event == "" {
		event = consts.WebhookEventCardTransactionAuthorization
	}
	if event != consts.WebhookEventCardTransactionAuthorization && event != consts.WebhookEventCardTransaction {
		return nil, badRequest("unsupported event " + event + "; use " +
			consts.WebhookEventCardTransactionAuthorization + " or " + consts.WebhookEventCardTransaction)
	}

	seqID, err := h.store.NextCardTransactionSeqID()
	if err != nil {
		logger.Error("failed to allocate card transaction sequence id", zap.Error(err))
		return nil, internalErr("failed to allocate transaction id")
	}

	tx, raw, txID, err := materialiseCardTransaction(scenario.Payload, req.Overrides, seqID, req.CardID)
	if err != nil {
		logger.Error("failed to materialise card transaction", zap.String("scenario", scenario.Key), zap.Error(err))
		return nil, internalErr("failed to build transaction from scenario")
	}

	if err := h.store.CreateCardTransaction(tx); err != nil {
		logger.Error("failed to create simulated card transaction", zap.Error(err))
		return nil, internalErr("failed to create transaction")
	}
	if err := h.store.StoreRawCardTransaction(txID, raw); err != nil {
		logger.Error("failed to store raw simulated card transaction", zap.Error(err))
		return nil, internalErr("failed to store transaction payload")
	}
	if err := h.store.AddCardTransactionIndex(req.CardID, txID); err != nil {
		logger.Warn("failed to index simulated card transaction", zap.String("card_id", req.CardID), zap.Error(err))
	}

	h.emitCardTransactionWebhook(event, req, raw, txID)

	logger.Info("simulated card transaction",
		zap.String("scenario", scenario.Key),
		zap.String("user_id", req.UserID),
		zap.String("card_id", req.CardID),
		zap.String("tx_id", txID),
		zap.Int("seq_id", seqID),
		zap.String("event", event),
		zap.String("card_status", card.Status),
	)

	return &SimulateCardTransactionResult{
		Scenario:      scenario.Key,
		Event:         event,
		TransactionID: txID,
		Transaction:   raw,
	}, nil
}

// emitCardTransactionWebhook sends the webhook for a simulated transaction.
// The two supported events carry different shapes and are not interchangeable:
// the authorization event carries the whole transaction, the notification event
// carries only identifiers.
func (h *Handler) emitCardTransactionWebhook(event string, req SimulateCardTransactionRequest, raw json.RawMessage, txID string) {
	switch event {
	case consts.WebhookEventCardTransaction:
		notification := req.Notification
		if notification == nil {
			notification = &CardTxNotification{Title: "Card transaction", Body: "A card transaction was recorded"}
		}
		h.webhookManager.SendAsync(event, req.UserID, map[string]interface{}{
			"title":         notification.Title,
			"body":          notification.Body,
			"transactionId": txID,
			"cardId":        req.CardID,
		}, 0)

	default:
		// authorizationData carries the transaction verbatim, so a consumer
		// sees exactly the payload it would get from the real provider.
		var authorizationData map[string]interface{}
		if err := json.Unmarshal(raw, &authorizationData); err != nil {
			logger.Error("failed to decode transaction for webhook", zap.Error(err))
			return
		}
		h.webhookManager.SendAsync(event, req.UserID, map[string]interface{}{
			"authorizationData": authorizationData,
		}, 0)
	}
}

// SetCardTransactionStatus drives a transaction through its status transitions,
// which is how a test moves a PROCESSING authorization to COMPLETED.
// POST /admin/card-transactions/{txID}/status
func (h *Handler) SetCardTransactionStatus(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	if txID == "" {
		h.sendError(w, http.StatusBadRequest, "txID is required")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == "" {
		h.sendError(w, http.StatusBadRequest, "status is required")
		return
	}
	if !isKnownCardTxStatus(req.Status) {
		h.sendError(w, http.StatusBadRequest, "unknown status "+req.Status)
		return
	}

	if _, err := h.store.GetCardTransaction(txID); err != nil {
		h.sendError(w, http.StatusNotFound, "card transaction not found")
		return
	}

	if err := h.store.UpdateCardTransactionStatus(txID, req.Status); err != nil {
		logger.Error("failed to update card transaction status", zap.String("tx_id", txID), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "failed to update status")
		return
	}

	logger.Info("card transaction status updated", zap.String("tx_id", txID), zap.String("status", req.Status))
	h.sendJSON(w, http.StatusOK, map[string]string{
		"transactionId": txID,
		"txStatus":      req.Status,
	})
}

func isKnownCardTxStatus(status string) bool {
	switch status {
	case consts.CardTxStatusProcessing, consts.CardTxStatusCompleted,
		consts.CardTxStatusReversed, consts.CardTxStatusDeclined:
		return true
	}
	return false
}

func scenarioKeys() []string {
	scenarios := listCardScenarios()
	keys := make([]string, 0, len(scenarios))
	for _, s := range scenarios {
		keys = append(keys, s.Key)
	}
	sort.Strings(keys)
	return keys
}

// materialiseCardTransaction turns a catalogue payload into a stored
// transaction. It returns the typed record, the raw JSON that will be served
// back verbatim, and the generated transaction id.
//
// The identity fields are always taken from this service rather than from the
// fixture: a fixture's transactionId and id are shared by every caller that
// picks the same scenario, and its cardId belongs to whichever card the fixture
// was captured against.
func materialiseCardTransaction(payload json.RawMessage, overrides map[string]interface{}, seqID int, cardUUID string) (*models.CardTransaction, json.RawMessage, string, error) {
	var fields map[string]interface{}
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, nil, "", err
	}

	// Caller overrides first, so the identity fields below always win.
	for k, v := range overrides {
		fields[k] = v
	}

	txID := utils.GenerateUUID()
	fields["transactionId"] = txID
	fields["id"] = seqID
	fields["cardId"] = numericCardID(cardUUID)
	fields["createdAt"] = time.Now().UTC().Format(time.RFC3339)

	normalised, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, "", err
	}

	var tx models.CardTransaction
	if err := json.Unmarshal(normalised, &tx); err != nil {
		return nil, nil, "", err
	}

	return &tx, normalised, txID, nil
}

// numericCardID derives GateHub's integer card id from our card UUID. Consumers
// see a number on the transaction, and it has to be the same number every time
// for a given card, so it is hashed rather than counted. The value is kept well
// inside int32 so it round-trips through JSON numbers safely.
func numericCardID(cardUUID string) int {
	if cardUUID == "" {
		return 0
	}
	sum := sha256.Sum256([]byte(cardUUID))
	// Mask to 31 bits, then keep 1 as the floor so a card never gets id 0.
	n := binary.BigEndian.Uint32(sum[:4]) & 0x7FFFFFFF
	if n == 0 {
		return 1
	}
	return int(n)
}
