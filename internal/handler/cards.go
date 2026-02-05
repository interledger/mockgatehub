package handler

import (
	"net/http"

	"mockgatehub/internal/logger"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// Card endpoint stubs - minimal implementation for sandbox

// CreateManagedCustomer creates a card customer (stub)
func (h *Handler) CreateManagedCustomer(w http.ResponseWriter, r *http.Request) {
	logger.Info("create managed customer called (stub)")
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"walletAddress": "mock-wallet-address",
		"customers": map[string]interface{}{
			"id":   "mock-customer-id",
			"code": "CUST001",
			"type": "Citizen",
			"accounts": []map[string]interface{}{
				{
					"id":       "mock-account-id",
					"currency": "EUR",
					"cards": []map[string]interface{}{
						{
							"id":     "mock-card-id",
							"status": "active",
							"type":   "virtual",
							"last4":  "1234",
						},
					},
				},
			},
		},
	})
}

// CreateCard creates a new card (stub)
func (h *Handler) CreateCard(w http.ResponseWriter, r *http.Request) {
	logger.Info("create card called (stub)")
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"id":     "mock-card-id",
		"status": "active",
		"type":   "virtual",
		"last4":  "1234",
	})
}

// GetCard retrieves card details (stub)
func (h *Handler) GetCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("get card called (stub)", zap.String("card_id", cardID))

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"id":     cardID,
		"status": "active",
		"type":   "virtual",
		"last4":  "1234",
	})
}

// DeleteCard deletes a card (stub)
func (h *Handler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	_ = chi.URLParam(r, "cardID")
	// Card deleted

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Card deleted successfully",
	})
}

// GetPendingConfirmations retrieves pending 3DS confirmations (stub)
// Returns empty list for local development since there are no real 3DS challenges
func (h *Handler) GetPendingConfirmations(w http.ResponseWriter, r *http.Request) {
	logger.Info("get pending confirmations called (stub)")
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"pendingConfirmations": []interface{}{},
	})
}
