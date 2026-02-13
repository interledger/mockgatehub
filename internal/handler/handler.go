package handler

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/utils"
	"mockgatehub/internal/webhook"

	"go.uber.org/zap"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	store          storage.Storage
	webhookManager *webhook.Manager
	tokenToUser    sync.Map // Maps bearer tokens to user UUIDs
}

// NewHandler creates a new handler with dependencies
func NewHandler(store storage.Storage, webhookManager *webhook.Manager) *Handler {
	logger.Info("initializing http handlers")
	return &Handler{
		store:          store,
		webhookManager: webhookManager,
	}
}

// RequestLogger middleware logs all incoming requests
func (h *Handler) RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logger.Info("request incoming",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("user_agent", r.UserAgent()),
		)

		// Log query parameters
		if len(r.URL.Query()) > 0 {
			logger.Debug("query parameters", zap.Any("params", r.URL.Query()))
		}

		// Log important headers
		if contentType := r.Header.Get("Content-Type"); contentType != "" {
			logger.Debug("content type", zap.String("content_type", contentType))
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			logger.Debug("authorization header present")
		}

		next.ServeHTTP(w, r)

		duration := time.Since(start)
		logger.Info("request completed",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("duration", duration),
		)
	})
}

// HealthCheck handles the health check endpoint
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	logger.Debug("health check requested")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"mockgatehub"}`))
}

// RootHandler serves the main iframe page for deposit/onboarding
func (h *Handler) RootHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("root handler requested", zap.String("url", r.URL.String()), zap.String("host", r.Host), zap.String("referer", r.Header.Get("Referer")))

	paymentType := r.URL.Query().Get("paymentType")
	bearer := r.URL.Query().Get("bearer")

	if bearer == "" {
		logger.Error("missing bearer token in root request", zap.String("url", r.URL.String()))
		http.Error(w, "Missing bearer token", http.StatusBadRequest)
		return
	}

	logger.Info("serving iframe", zap.String("payment_type", paymentType), zap.String("bearer_prefix", bearer[:min(20, len(bearer))]))

	// If no paymentType is provided, treat this as onboarding and serve the KYC iframe
	if paymentType == "" || paymentType == "onboarding" {
		// Try to extract user UUID from bearer token mapping
		userUUID := h.extractUserFromBearer(bearer)

		// If not found in mapping, try to get from query params (user_id might be passed from frontend)
		if userUUID == "" {
			userUUID = r.URL.Query().Get("user_id")
		}

		if userUUID == "" {
			logger.Warn("could not extract user from bearer token or query params, will rely on form submission")
		}

		// Load KYC iframe template
		kycTemplatePath := filepath.Join("web", "kyc-iframe.html")
		kycTmpl, err := template.ParseFiles(kycTemplatePath)
		if err != nil {
			logger.Error("failed to parse kyc iframe template", zap.Error(err))
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}

		// Prepare data for KYC template - pass bearer token to be used on submission
		kycData := map[string]string{
			"Token":  bearer,
			"UserID": userUUID,
		}

		// Set headers
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Render KYC iframe
		if err := kycTmpl.Execute(w, kycData); err != nil {
			logger.Error("failed to execute kyc iframe template", zap.Error(err))
			http.Error(w, "Template execution error", http.StatusInternalServerError)
			return
		}
		return
	}

	// Otherwise, serve the generic payment iframe (deposit/withdrawal/exchange)
	bearerShort := bearer
	if len(bearer) > 20 {
		bearerShort = bearer[:20] + "..."
	}

	// Load template from web folder
	templatePath := filepath.Join("web", "index.html")
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		logger.Error("failed to parse template", zap.Error(err))
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	// Prepare data for template
	data := map[string]string{
		"PaymentType": paymentType,
		"Bearer":      bearer,
		"BearerShort": bearerShort,
	}

	// Set headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Render template
	if err := tmpl.Execute(w, data); err != nil {
		logger.Error("failed to execute template", zap.Error(err))
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return
	}
}

// TransactionCompleteHandler handles transaction completion callbacks from the iframe
func (h *Handler) TransactionCompleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
		return
	}

	logger.Info("transaction complete handler requested")

	paymentType := r.URL.Query().Get("paymentType")
	bearer := r.URL.Query().Get("bearer")

	if bearer == "" {
		logger.Error("missing bearer token in transaction completion")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"Missing bearer token"}`))
		return
	}

	logger.Info("transaction completed", zap.String("payment_type", paymentType))

	// Parse request body for transaction details (amount, currency, etc.)
	type TransactionRequest struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}

	var txReq TransactionRequest
	// Default values if body is empty or parsing fails
	txReq.Amount = "100.00"
	txReq.Currency = "USD"

	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil && len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &txReq); err == nil {
				logger.Info("parsed transaction details", zap.String("amount", txReq.Amount), zap.String("currency", txReq.Currency))
			} else {
				logger.Warn("failed to parse request body, using defaults", zap.Error(err))
			}
		}
	}

	// For deposit type, create a transaction and send a webhook to wallet-backend
	if paymentType == "deposit" {
		// Decode bearer to get user UUID
		// In a real implementation, we would validate the JWT token
		// For mock purposes, we extract the user UUID from a simple format
		userUUID := h.extractUserFromBearer(bearer)

		if userUUID != "" {
			// Get user to find their wallet address
			user, err := h.store.GetUser(userUUID)
			if err == nil && user != nil {
				// Get user's wallets to find the deposit address
				wallets, err := h.store.GetWalletsByUser(userUUID)
				if err == nil && len(wallets) > 0 {
					// Use the first wallet's address
					walletAddress := wallets[0].Address

					// Get vault_uuid for the currency (from consts)
					if txReq.Currency == "" {
						txReq.Currency = "USD"
					}

					vaultUUID := consts.SandboxVaultIDs[txReq.Currency]
					if vaultUUID == "" {
						// Fallback to USD vault if currency not found
						logger.Warn("unknown currency, using usd vault", zap.String("requested_currency", txReq.Currency))
						txReq.Currency = "USD"
						vaultUUID = consts.SandboxVaultIDs[txReq.Currency]
					}

					amountFloat, err := strconv.ParseFloat(txReq.Amount, 64)
					if err != nil {
						logger.Warn("invalid amount, defaulting to 100.00", zap.String("amount", txReq.Amount), zap.Error(err))
						amountFloat = 100.00
					}
					amountStr := fmt.Sprintf("%.2f", amountFloat)
					feeStr := "0.00"            // No fees in sandbox
					totalAmountStr := amountStr // Total = amount + fees

					txID := utils.GenerateUUID()

					tx := &models.Transaction{
						ID:               txID,
						UserID:           userUUID,
						Amount:           amountStr,
						TotalAmount:      totalAmountStr,
						Fee:              feeStr,
						Currency:         txReq.Currency,
						VaultUUID:        vaultUUID,
						ReceivingAddress: walletAddress,
						Type:             consts.TransactionTypeDeposit,
						DepositType:      consts.DepositTypeExternal,
						Status:           1, // 1 = completed
					}

					if err := h.store.CreateTransaction(tx); err != nil {
						logger.Error("failed to create transaction", zap.String("transaction_id", txID), zap.Error(err))
					}

					if err := h.store.AddBalance(userUUID, txReq.Currency, amountFloat); err != nil {
						logger.Error("failed to update balance for user", zap.String("user_id", userUUID), zap.Error(err))
					}

					// Send deposit webhook (matches GateHub webhook spec) with dynamic values
					h.webhookManager.SendAsync("core.deposit.completed", userUUID, map[string]interface{}{
						"tx_uuid":      txID,
						"amount":       amountStr,      // From iframe form
						"currency":     txReq.Currency, // From iframe form
						"vault_uuid":   vaultUUID,      // Vault UUID for this currency
						"address":      walletAddress,  // The wallet address that received the deposit
						"deposit_type": "external",     // External deposit type (lowercase per spec)
						"total_fees":   "0",            // Fees charged (matches GateHub spec)
					})

					logger.Info("sent deposit webhook", zap.String("user_id", userUUID), zap.String("amount", amountStr), zap.String("currency", txReq.Currency), zap.String("wallet_address", walletAddress))
				} else {
					logger.Error("no wallets found for user", zap.String("user_id", userUUID))
				}
			} else {
				logger.Error("user not found", zap.String("user_id", userUUID), zap.Error(err))
			}
		} else {
			logger.Warn("could not extract user uuid from bearer token")
		}
	}

	// Return success response
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success","message":"Transaction completed"}`))
}

// extractUserFromBearer extracts the user UUID from the bearer token
func (h *Handler) extractUserFromBearer(bearer string) string {
	// Look up the user UUID from the token mapping
	if userUUID, ok := h.tokenToUser.Load(bearer); ok {
		if uuid, ok := userUUID.(string); ok {
			return uuid
		}
	}

	logger.Debug("bearer token not found in mapping")
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
