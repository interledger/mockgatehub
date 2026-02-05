package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"mockgatehub/internal/logger"
	"mockgatehub/internal/utils"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// Card endpoint stubs - minimal implementation for sandbox

// CreateCustomer creates a card customer (generic endpoint)
func (h *Handler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	logger.Info("create customer called", zap.String("path", r.URL.Path), zap.String("method", r.Method))

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Extract customer details from request
	walletAddress, _ := req["walletAddress"].(string)
	nameOnCard, _ := req["nameOnCard"].(string)
	account, _ := req["account"].(map[string]interface{})

	accountCurrency := "EUR"
	if account != nil {
		if currency, ok := account["currency"].(string); ok {
			accountCurrency = currency
		}
	}

	response := map[string]interface{}{
		"walletAddress": walletAddress,
		"customers": map[string]interface{}{
			"id":         utils.GenerateUUID(),
			"code":       "CUST" + utils.GenerateUUID()[:4],
			"type":       "Citizen",
			"nameOnCard": nameOnCard,
			"accounts": []map[string]interface{}{
				{
					"id":       utils.GenerateUUID(),
					"currency": accountCurrency,
					"cards": []map[string]interface{}{
						{
							"id":     utils.GenerateUUID(),
							"status": "active",
							"type":   "physical",
							"last4":  "1234",
						},
					},
				},
			},
		},
	}

	h.sendJSON(w, http.StatusCreated, response)
}

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

// ListCards retrieves cards for a customer (stub)
func (h *Handler) ListCards(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	logger.Info("list cards called (stub)", zap.String("customer_id", customerID))

	cards := []map[string]interface{}{
		{
			"id":     utils.GenerateUUID(),
			"status": "active",
			"type":   "physical",
			"last4":  "1234",
		},
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"data": cards,
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
		"id":               cardID,
		"status":           "active",
		"type":             "virtual",
		"last4":            "1234",
		"maskedPan":        "****1234",
		"expiryDate":       "12/28",
		"nameOnCard":       "Test User",
		"productCode":      "PROD_VIRTUAL_CARD",
		"relationType":     "PRIMARY",
		"OrderPlasticSync": false,
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
// Returns mock pending confirmation for testing
func (h *Handler) GetPendingConfirmations(w http.ResponseWriter, r *http.Request) {
	logger.Info("get pending confirmations called (stub)")
	
	// Return a mock pending confirmation for testing purposes
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"pendingConfirmations": []map[string]interface{}{
			{
				"transactionId":     "3ds-" + utils.GenerateUUID()[:8],
				"merchantName":      "Test Merchant",
				"purchaseAmount":    "15.00",
				"purchaseCurrency":  "EUR",
				"status":            "pending",
				"createdAt":         time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
}

// CreateCustomerAddress creates a delivery address for a card customer
func (h *Handler) CreateCustomerAddress(w http.ResponseWriter, r *http.Request) {
	logger.Info("creating customer address", zap.String("path", r.URL.Path), zap.String("method", r.Method))
	customerID := chi.URLParam(r, "customerID")
	logger.Info("customer address path params", zap.String("customer_id", customerID))

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	address := map[string]interface{}{
		"id":          utils.GenerateUUID(),
		"customerID":  customerID,
		"type":        req["type"],
		"countryCode": req["countryCode"],
		"line1":       req["line1"],
		"city":        req["city"],
		"zipCode":     req["zipCode"],
		"reason":      req["reason"],
		"createdAt":   time.Now().UTC().Format(time.RFC3339),
	}

	// Return as array of addresses
	h.sendJSON(w, http.StatusCreated, []map[string]interface{}{address})
}

// GetCustomerAddresses retrieves delivery addresses for a card customer
func (h *Handler) GetCustomerAddresses(w http.ResponseWriter, r *http.Request) {
	logger.Info("getting customer addresses", zap.String("path", r.URL.Path), zap.String("method", r.Method))
	customerID := chi.URLParam(r, "customerID")
	logger.Info("customer address path params", zap.String("customer_id", customerID))

	addresses := []map[string]interface{}{
		{
			"id":          utils.GenerateUUID(),
			"customerID":  customerID,
			"type":        "DELIVERY",
			"countryCode": "USA",
			"line1":       "123 Main St",
			"city":        "NYC",
			"zipCode":     "10001",
			"createdAt":   time.Now().UTC().Format(time.RFC3339),
		},
	}

	h.sendJSON(w, http.StatusOK, addresses)
}

// OrderAdditionalCard orders an additional card for an account
func (h *Handler) OrderAdditionalCard(w http.ResponseWriter, r *http.Request) {
	logger.Info("ordering additional card", zap.String("path", r.URL.Path), zap.String("method", r.Method))
	accountID := chi.URLParam(r, "accountID")
	logger.Info("additional card path params", zap.String("account_id", accountID))

	card := map[string]interface{}{
		"id":         utils.GenerateUUID(),
		"accountID":  accountID,
		"status":     "PENDING",
		"cardNumber": "****1234",
		"createdAt":  time.Now().UTC().Format(time.RFC3339),
	}

	h.sendJSON(w, http.StatusCreated, card)
}

// GetCardLimits retrieves spending limits for a card (stub)
func (h *Handler) GetCardLimits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("get card limits called", zap.String("card_id", cardID))

	limits := []map[string]interface{}{
		{
			"type":       "dailyOverall",
			"limit":      1000.00,
			"currency":   "EUR",
			"isDisabled": false,
		},
		{
			"type":       "perTransaction",
			"limit":      500.00,
			"currency":   "EUR",
			"isDisabled": false,
		},
		{
			"type":       "monthlyOverall",
			"limit":      5000.00,
			"currency":   "EUR",
			"isDisabled": false,
		},
	}

	h.sendJSON(w, http.StatusOK, limits)
}

// UpdateCardLimits updates spending limits for a card (stub)
func (h *Handler) UpdateCardLimits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("update card limits called", zap.String("card_id", cardID))

	var limits []map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&limits); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Return the updated limits
	h.sendJSON(w, http.StatusOK, limits)
}

// GetCardToken generates a token for retrieving card data (stub)
func (h *Handler) GetCardToken(w http.ResponseWriter, r *http.Request) {
	logger.Info("get card token called")

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token": "mock-card-token-" + utils.GenerateUUID(),
		"links": []map[string]interface{}{
			{
				"href":   "https://secure-card-service.example.com/v1/card-data",
				"rel":    "card-data",
				"method": "GET",
			},
		},
	})
}

// CreateCardTransaction creates a card transaction (stub)
func (h *Handler) CreateCardTransaction(w http.ResponseWriter, r *http.Request) {
	logger.Info("create card transaction called")

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	response := map[string]interface{}{
		"id":                  utils.GenerateUUID(),
		"transactionId":       "tx-" + utils.GenerateUUID()[:8],
		"type":                1,
		"txStatus":            "completed",
		"transactionAmount":   req["amount"],
		"transactionCurrency": req["currency"],
		"billingAmount":       req["amount"],
		"billingCurrency":     req["currency"],
		"merchantName":        req["merchantName"],
		"transactionDateTime": time.Now().UTC().Format(time.RFC3339),
		"cardScheme":          2,
		"ghResponseCode":      "00",
	}

	h.sendJSON(w, http.StatusCreated, response)
}

// ListCardTransactions retrieves card transactions (stub)
func (h *Handler) ListCardTransactions(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("list card transactions called", zap.String("card_id", cardID))

	transactions := []map[string]interface{}{
		{
			"id":                  1,
			"transactionId":       "tx-" + utils.GenerateUUID()[:8],
			"type":                0,
			"txStatus":            "completed",
			"transactionAmount":   "12.50",
			"transactionCurrency": "EUR",
			"billingAmount":       "12.50",
			"billingCurrency":     "EUR",
			"merchantName":        "Test Store",
			"transactionDateTime": time.Now().UTC().Format(time.RFC3339),
			"cardScheme":          2,
			"ghResponseCode":      "00",
		},
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"data": transactions,
		"pagination": map[string]interface{}{
			"pageNumber":   1,
			"pageSize":     20,
			"totalPages":   1,
			"totalRecords": len(transactions),
		},
	})
}

// CreateThreeDSChallenge creates a 3DS challenge for testing (stub)
func (h *Handler) CreateThreeDSChallenge(w http.ResponseWriter, r *http.Request) {
	logger.Info("create 3ds challenge called")

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.sendJSON(w, http.StatusCreated, map[string]interface{}{
		"transactionId": "3ds-" + utils.GenerateUUID()[:8],
		"status":        "pending",
		"challengeUrl":  "/cards/v1/test/3ds/challenge/" + utils.GenerateUUID(),
	})
}

// ConfirmThreeDS confirms or denies a 3DS challenge (stub)
func (h *Handler) ConfirmThreeDS(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	logger.Info("confirm 3ds called", zap.String("tx_id", txID))

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	confirmed, _ := req["confirmed"].(bool)
	status := "declined"
	if confirmed {
		status = "approved"
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"transactionId": txID,
		"status":        status,
	})
}

// GetCardApplicationProducts retrieves available card products (stub)
func (h *Handler) GetCardApplicationProducts(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	logger.Info("get card application products called", zap.String("app_id", appID))

	products := []map[string]interface{}{
		{
			"uuid":               utils.GenerateUUID(),
			"accountProductCode": "PROD_EUR_ACCOUNT",
			"code":               "PROD_VIRTUAL_CARD",
			"name":               "Virtual Debit Card",
			"cost":               "0.00",
			"cardProductLimits": []map[string]interface{}{
				{
					"type":       "dailyOverall",
					"currency":   "EUR",
					"limit":      "1000.00",
					"isDisabled": false,
				},
			},
		},
		{
			"uuid":               utils.GenerateUUID(),
			"accountProductCode": "PROD_EUR_ACCOUNT",
			"code":               "PROD_PLASTIC_CARD",
			"name":               "Physical Debit Card",
			"cost":               "5.00",
			"cardProductLimits": []map[string]interface{}{
				{
					"type":       "dailyOverall",
					"currency":   "EUR",
					"limit":      "1000.00",
					"isDisabled": false,
				},
			},
		},
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"data": products,
	})
}

// OrderPlasticCard orders a physical card (stub)
func (h *Handler) OrderPlasticCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("order plastic card called", zap.String("card_id", cardID))

	h.sendJSON(w, http.StatusCreated, map[string]interface{}{
		"orderId":   utils.GenerateUUID(),
		"cardId":    cardID,
		"status":    "PENDING",
		"type":      "PLASTIC",
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"deliveryAddress": map[string]interface{}{
			"firstName":   "John",
			"lastName":    "Doe",
			"countryCode": "USA",
			"line1":       "123 Main St",
			"city":        "NYC",
			"zipCode":     "10001",
		},
	})
}
