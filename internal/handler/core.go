package handler

import (
	"fmt"
	"net/http"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"
	"mockgatehub/internal/utils"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req models.CreateWalletRequest
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get userID from path parameter if not in body
	userID := chi.URLParam(r, "userID")
	if req.UserID == "" && userID != "" {
		req.UserID = userID
	}

	if req.UserID == "" {
		h.sendError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	logger.Info.Printf("Creating wallet for user: %s", req.UserID)

	_, err := h.store.GetUser(req.UserID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "User not found")
		return
	}

	address := utils.GenerateMockXRPLAddress()

	if req.Type == 0 {
		req.Type = consts.WalletTypeStandard
	}
	if req.Network == 0 {
		req.Network = consts.NetworkXRPLedger
	}

	wallet := &models.Wallet{
		Address: address,
		UserID:  req.UserID,
		Name:    req.Name,
		Type:    req.Type,
		Network: req.Network,
	}

	if err := h.store.CreateWallet(wallet); err != nil {
		logger.Error.Printf("Failed to create wallet: %v", err)
		h.sendError(w, http.StatusInternalServerError, "Failed to create wallet")
		return
	}

	logger.Info.Printf("Created wallet: %s for user %s", address, req.UserID)
	h.sendJSON(w, http.StatusCreated, wallet)
}

// GetUserWallets retrieves all wallets for a user (GET /core/v1/users/{userID})
// If no wallets exist, creates one automatically
func (h *Handler) GetUserWallets(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	logger.Info.Printf("Getting wallets for user: %s", userID)

	_, err := h.store.GetUser(userID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "User not found")
		return
	}

	wallets, err := h.store.GetWalletsByUser(userID)
	if err != nil {
		logger.Error.Printf("Failed to get user wallets: %v", err)
		h.sendError(w, http.StatusInternalServerError, "Failed to get wallets")
		return
	}

	// If no wallets exist, create one automatically
	if len(wallets) == 0 {
		logger.Info.Printf("No wallets found for user %s, creating one automatically", userID)
		address := utils.GenerateMockXRPLAddress()
		wallet := &models.Wallet{
			Address: address,
			UserID:  userID,
			Name:    "Default Wallet",
			Type:    consts.WalletTypeStandard,
			Network: consts.NetworkXRPLedger,
		}

		if err := h.store.CreateWallet(wallet); err != nil {
			logger.Error.Printf("Failed to create wallet: %v", err)
			h.sendError(w, http.StatusInternalServerError, "Failed to create wallet")
			return
		}

		logger.Info.Printf("Created default wallet %s for user %s", address, userID)
		wallets = append(wallets, wallet)
	}

	// Return in the format expected by wallet-backend: { wallets: [...] }
	response := models.UserWalletsResponse{}
	response.Wallets = make([]models.UserWalletResponse, 0, len(wallets))

	for i, w := range wallets {
		response.Wallets = append(response.Wallets, models.UserWalletResponse{
			UUID:    w.Address,
			Address: w.Address,
			Name:    w.Name,
			Type:    w.Type,
			Primary: i == 0,
			Active:  true,
			Enabled: true,
		})
	}

	logger.Info.Printf("Returning %d wallets for user %s", len(wallets), userID)
	h.sendJSON(w, http.StatusOK, response)
}

func (h *Handler) GetWallet(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletID")
	if walletID == "" {
		// Try legacy parameter name
		walletID = chi.URLParam(r, "address")
	}
	if walletID == "" {
		h.sendError(w, http.StatusBadRequest, "Wallet address is required")
		return
	}

	logger.Info.Printf("Getting wallet: %s", walletID)

	wallet, err := h.store.GetWallet(walletID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Wallet not found")
		return
	}

	h.sendJSON(w, http.StatusOK, wallet)
}

func (h *Handler) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletID")
	logger.Info.Printf("DEBUG: walletID from path = '%s'", walletID)

	if walletID == "" {
		// Try legacy parameter name
		walletID = chi.URLParam(r, "address")
		logger.Info.Printf("DEBUG: walletID from address = '%s'", walletID)
	}
	if walletID == "" {
		h.sendError(w, http.StatusBadRequest, "Wallet address is required")
		return
	}

	logger.Info.Printf("Getting balance for wallet: %s", walletID)

	wallet, err := h.store.GetWallet(walletID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Wallet not found")
		return
	}

	balances := make([]models.WalletBalanceResponse, 0, len(consts.SandboxCurrencies))
	for _, currency := range consts.SandboxCurrencies {
		balance, _ := h.store.GetBalance(wallet.UserID, currency)
		balances = append(balances, models.WalletBalanceResponse{
			Available: fmt.Sprintf("%g", balance),
			Pending:   "0",
			Total:     fmt.Sprintf("%g", balance),
			Vault: models.VaultSummary{
				UUID:      consts.SandboxVaultIDs[currency],
				Name:      fmt.Sprintf("Sandbox Vault %s", currency),
				AssetCode: currency,
				CreatedAt: time.Now().Format(time.RFC3339),
				UpdatedAt: time.Now().Format(time.RFC3339),
			},
		})
	}

	logger.Info.Printf("Returning %d currency balances for wallet %s", len(balances), walletID)

	h.sendJSON(w, http.StatusOK, balances)
}

func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTransactionRequest
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// If user_id not in body, try to get from x-gatehub-managed-user-uuid header
	if req.UserID == "" {
		req.UserID = r.Header.Get("x-gatehub-managed-user-uuid")
		logger.Info.Printf("CreateTransaction: attempting to extract user_id from header. Got: %s", req.UserID)
	}

	// If still no user_id, try to look up from receiving_address (wallet)
	if req.UserID == "" && req.ReceivingAddress != "" {
		wallet, err := h.store.GetWallet(req.ReceivingAddress)
		if err == nil && wallet != nil {
			req.UserID = wallet.UserID
			logger.Info.Printf("CreateTransaction: resolved user_id '%s' from receiving_address '%s'", req.UserID, req.ReceivingAddress)
		}
	}

	if req.UserID == "" {
		h.sendError(w, http.StatusBadRequest, "user_id is required (provide in body, x-gatehub-managed-user-uuid header, or use a valid receiving_address)")
		return
	}
	if req.Amount <= 0 {
		h.sendError(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	// Infer currency from vault_uuid if not provided
	if req.Currency == "" {
		if req.VaultUUID == "" {
			h.sendError(w, http.StatusBadRequest, "either currency or vault_uuid is required")
			return
		}
		// Look up currency from vault_uuid
		currency, exists := consts.VaultUUIDToCurrency[req.VaultUUID]
		if !exists {
			h.sendError(w, http.StatusBadRequest, "invalid vault_uuid")
			return
		}
		req.Currency = currency
		logger.Info.Printf("Inferred currency '%s' from vault_uuid '%s'", currency, req.VaultUUID)
	} else {
		// If currency is provided, ensure vault_uuid matches (if also provided)
		if req.VaultUUID != "" {
			expectedVaultUUID := consts.SandboxVaultIDs[req.Currency]
			if req.VaultUUID != expectedVaultUUID {
				logger.Warn.Printf("Vault UUID mismatch: got %s, expected %s for currency %s. Using vault_uuid to determine currency.",
					req.VaultUUID, expectedVaultUUID, req.Currency)
				// Trust vault_uuid over currency parameter
				if inferredCurrency, exists := consts.VaultUUIDToCurrency[req.VaultUUID]; exists {
					req.Currency = inferredCurrency
					logger.Info.Printf("Corrected currency to '%s' based on vault_uuid", inferredCurrency)
				}
			}
		}
	}

	logger.Info.Printf("Creating transaction: user=%s, amount=%.2f %s, type=%d",
		req.UserID, req.Amount, req.Currency, req.Type)

	_, err := h.store.GetUser(req.UserID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "User not found")
		return
	}

	if req.Type == 0 {
		req.Type = consts.TransactionTypeDeposit
	}
	if req.DepositType == "" {
		if req.Type == consts.TransactionTypeDeposit {
			req.DepositType = consts.DepositTypeExternal
		} else {
			req.DepositType = consts.DepositTypeHosted
		}
	}

	// Ensure vault_uuid is set based on currency
	if req.VaultUUID == "" {
		req.VaultUUID = consts.SandboxVaultIDs[req.Currency]
	}

	// Format amounts as strings to match GateHub API
	amountStr := fmt.Sprintf("%.2f", req.Amount)
	feeStr := "0.00"            // Mock: no fees in sandbox
	totalAmountStr := amountStr // Total = amount + fee

	tx := &models.Transaction{
		UserID:           req.UserID,
		UID:              req.UID,
		Amount:           amountStr,
		TotalAmount:      totalAmountStr,
		Fee:              feeStr,
		Currency:         req.Currency,
		VaultUUID:        req.VaultUUID,
		ReceivingAddress: req.ReceivingAddress,
		Type:             req.Type,
		DepositType:      req.DepositType,
		Status:           1, // 1 = completed
	}

	if err := h.store.CreateTransaction(tx); err != nil {
		logger.Error.Printf("Failed to create transaction: %v", err)
		h.sendError(w, http.StatusInternalServerError, "Failed to create transaction")
		return
	}

	if err := h.store.AddBalance(req.UserID, req.Currency, req.Amount); err != nil {
		logger.Error.Printf("Failed to update balance: %v", err)
		h.sendError(w, http.StatusInternalServerError, "Failed to update balance")
		return
	}

	logger.Info.Printf("Created transaction: %s (%s %s)", tx.ID, tx.Amount, tx.Currency)

	if req.DepositType == consts.DepositTypeExternal {
		go h.webhookManager.SendAsync(consts.WebhookEventDepositCompleted, req.UserID, models.DepositWebhookData{
			TransactionID: tx.ID,
			Amount:        tx.Amount, // Already a string
			Currency:      tx.Currency,
		})
	}

	h.sendJSON(w, http.StatusCreated, tx)
}

func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	if txID == "" {
		h.sendError(w, http.StatusBadRequest, "Transaction ID is required")
		return
	}

	logger.Info.Printf("Getting transaction: %s", txID)

	tx, err := h.store.GetTransaction(txID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Transaction not found")
		return
	}

	h.sendJSON(w, http.StatusOK, tx)
}

// GetUserCurrencies returns the list of currencies the user has accounts for
func (h *Handler) GetUserCurrencies(w http.ResponseWriter, r *http.Request) {
	bearer := r.URL.Query().Get("bearer")
	if bearer == "" {
		bearer = r.Header.Get("Authorization")
		if len(bearer) > 7 && bearer[:7] == "Bearer " {
			bearer = bearer[7:]
		}
	}

	if bearer == "" {
		h.sendError(w, http.StatusBadRequest, "Missing bearer token")
		return
	}

	// Extract user UUID from bearer token
	userUUID := h.extractUserFromBearer(bearer)
	if userUUID == "" {
		// Return default currencies if we can't determine user
		logger.Warn.Println("[HANDLER] Could not extract user from bearer, returning all currencies")
		h.sendJSON(w, http.StatusOK, models.CurrenciesResponse{
			Currencies: []string{"USD", "EUR", "CAD", "GBP", "JPY", "AUD", "CHF", "CNY", "INR", "AED", "PEB", "XRP"},
		})
		return
	}

	logger.Info.Printf("[HANDLER] Getting currencies for user: %s", userUUID)

	// Get currencies that have non-zero balances for this user
	allCurrencies := []string{"USD", "EUR", "CAD", "GBP", "JPY", "AUD", "CHF", "CNY", "INR", "AED", "PEB", "XRP"}
	userCurrencies := []string{}

	for _, currency := range allCurrencies {
		balance, err := h.store.GetBalance(userUUID, currency)
		if err == nil && balance > 0 {
			userCurrencies = append(userCurrencies, currency)
		}
	}

	// If no currencies with balance, return all currencies (user hasn't deposited yet)
	if len(userCurrencies) == 0 {
		logger.Info.Printf("[HANDLER] No balances found for user %s, returning all currencies", userUUID)
		h.sendJSON(w, http.StatusOK, models.CurrenciesResponse{
			Currencies: allCurrencies,
		})
		return
	}

	logger.Info.Printf("[HANDLER] Found %d currencies with balances for user %s: %v", len(userCurrencies), userUUID, userCurrencies)

	h.sendJSON(w, http.StatusOK, models.CurrenciesResponse{
		Currencies: userCurrencies,
	})
}
