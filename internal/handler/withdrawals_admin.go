package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// ListWithdrawals returns a user's withdrawals, most recent first, optionally
// filtered by status. A test needs to find the pending withdrawal it just
// created in order to settle it.
// GET /admin/users/{userID}/withdrawals?status=pending
func (h *Handler) ListWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "user ID is required")
		return
	}

	statusFilter := r.URL.Query().Get("status")
	wantStatus, err := withdrawalStatusFilter(statusFilter)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	transactions, err := h.store.ListTransactionsByUser(userID)
	if err != nil {
		logger.Error("failed to list transactions", zap.String("user_id", userID), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "failed to list transactions")
		return
	}

	withdrawals := make([]*models.Transaction, 0)
	for _, tx := range transactions {
		if tx.DepositType != consts.DepositTypeWithdrawal {
			continue
		}
		if wantStatus != nil && tx.Status != *wantStatus {
			continue
		}
		withdrawals = append(withdrawals, tx)
	}

	// ListTransactionsByUser already orders most recent first.
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"withdrawals": withdrawals,
		"count":       len(withdrawals),
	})
}

// withdrawalStatusFilter turns the query parameter into a status to match, or
// nil for no filtering. An unrecognised value is refused rather than silently
// returning everything, which would look like a passing assertion.
func withdrawalStatusFilter(filter string) (*int, error) {
	switch filter {
	case "":
		return nil, nil
	case "pending":
		status := consts.TransactionStatusPending
		return &status, nil
	case "completed":
		status := consts.TransactionStatusCompleted
		return &status, nil
	case "failed", "rejected":
		status := consts.TransactionStatusFailed
		return &status, nil
	default:
		return nil, fmt.Errorf("unknown status %q: expected pending, completed or failed", filter)
	}
}

// TriggerWithdrawalEvent settles a pending withdrawal, either completing it or
// rejecting it, and emits the corresponding webhook. This is the counterpart to
// asynchronous withdrawals: without it a pending withdrawal has no way to
// finish.
// POST /admin/withdrawals/{txID}/trigger-event
func (h *Handler) TriggerWithdrawalEvent(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	if txID == "" {
		h.sendError(w, http.StatusBadRequest, "transaction ID is required")
		return
	}

	var req struct {
		Event string `json:"event"`
	}
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.settleWithdrawal(txID, req.Event); err != nil {
		h.sendAPIError(w, err)
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]string{
		"transaction_id": txID,
		"event":          req.Event,
	})
}

// settleWithdrawal is the shared implementation behind both the settlement
// endpoint and the admin UI, so the two cannot drift apart.
func (h *Handler) settleWithdrawal(txID, event string) error {
	switch event {
	case consts.WebhookEventWithdrawalCompleted, consts.WebhookEventWithdrawalRejected:
	default:
		return badRequest("unsupported event " + event + "; use " +
			consts.WebhookEventWithdrawalCompleted + " or " + consts.WebhookEventWithdrawalRejected)
	}

	tx, err := h.store.GetTransaction(txID)
	if err != nil {
		return notFound("transaction not found")
	}
	if tx.DepositType != consts.DepositTypeWithdrawal {
		return badRequest("transaction is not a withdrawal")
	}

	// Settling twice would double-charge a completion or contradict an
	// already-published outcome.
	if tx.Status != consts.TransactionStatusPending {
		return conflict("withdrawal is not pending; it has already been settled")
	}

	if event == consts.WebhookEventWithdrawalCompleted {
		return h.completeWithdrawal(tx)
	}
	return h.rejectWithdrawal(tx)
}

// completeWithdrawal charges the balance and announces the completion. The
// balance moves here rather than at request time so that a rejection leaves it
// untouched.
func (h *Handler) completeWithdrawal(tx *models.Transaction) error {
	totalAmount, err := strconv.ParseFloat(tx.TotalAmount, 64)
	if err != nil {
		logger.Error("withdrawal has an unparseable total", zap.String("tx_id", tx.ID), zap.String("total", tx.TotalAmount), zap.Error(err))
		return internalErr("transaction total is not a number")
	}

	// Charge before publishing the outcome: announcing a completion we could
	// not charge would leave the consumer's ledger ahead of ours.
	if err := h.store.DeductBalance(tx.UserID, tx.Currency, totalAmount); err != nil {
		logger.Error("failed to deduct balance for withdrawal", zap.String("tx_id", tx.ID), zap.Error(err))
		return conflict("could not deduct balance: " + err.Error())
	}

	if err := h.store.UpdateTransactionStatus(tx.ID, consts.TransactionStatusCompleted); err != nil {
		logger.Error("failed to complete withdrawal", zap.String("tx_id", tx.ID), zap.Error(err))
		return internalErr("failed to update transaction status")
	}

	h.webhookManager.SendAsync(consts.WebhookEventWithdrawalCompleted, tx.UserID, map[string]interface{}{
		"tx_uuid":    tx.ID,
		"amount":     tx.Amount,
		"currency":   tx.Currency,
		"address":    tx.ReceivingAddress,
		"total_fees": tx.Fee,
	}, 0)

	logger.Info("withdrawal completed",
		zap.String("tx_id", tx.ID),
		zap.String("user_id", tx.UserID),
		zap.Float64("charged", totalAmount),
	)
	return nil
}

// rejectWithdrawal marks the withdrawal failed and announces it. No balance
// moves: the money was never taken.
func (h *Handler) rejectWithdrawal(tx *models.Transaction) error {
	if err := h.store.UpdateTransactionStatus(tx.ID, consts.TransactionStatusFailed); err != nil {
		logger.Error("failed to reject withdrawal", zap.String("tx_id", tx.ID), zap.Error(err))
		return internalErr("failed to update transaction status")
	}

	h.webhookManager.SendAsync(consts.WebhookEventWithdrawalRejected, tx.UserID, map[string]interface{}{
		"tx_uuid":  tx.ID,
		"user_id":  tx.UserID,
		"status":   "REJECTED",
		"currency": tx.Currency,
		"amount":   tx.Amount,
	}, 0)

	logger.Info("withdrawal rejected", zap.String("tx_id", tx.ID), zap.String("user_id", tx.UserID))
	return nil
}
