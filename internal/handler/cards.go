package handler

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/utils"

	"github.com/go-chi/chi/v5"
)

const managedUserHeader = "x-gatehub-managed-user-uuid"

// CreateManagedCustomer creates a card customer with EUR account and initial card
func (h *Handler) CreateManagedCustomer(w http.ResponseWriter, r *http.Request) {
	var req models.CreateCustomerAndCardArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := r.Header.Get(managedUserHeader)
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "x-gatehub-managed-user-uuid header is required")
		return
	}

	user, err := h.store.GetUser(userID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "managed user not found")
		return
	}

	if user.KYCState != consts.KYCStateAccepted {
		h.sendError(w, http.StatusBadRequest, "kyc not accepted")
		return
	}

	if req.NameOnCard == "" {
		h.sendError(w, http.StatusBadRequest, "nameOnCard is required")
		return
	}
	if len(req.NameOnCard) > 26 {
		h.sendError(w, http.StatusBadRequest, "nameOnCard exceeds 26 characters")
		return
	}

	currency := req.Account.Currency
	if currency == "" {
		currency = "EUR"
	}
	if currency != "EUR" {
		h.sendError(w, http.StatusBadRequest, "only EUR currency is supported")
		return
	}

	if req.Delivery != nil {
		if err := validateDeliveryAddress(*req.Delivery); err != nil {
			h.sendError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	customerID := utils.GenerateUUID()
	accountID := utils.GenerateUUID()
	cardID := utils.GenerateUUID()

	customer := models.Customer{
		ID:        &customerID,
		SourceID:  userID,
		Type:      "Citizen",
		Code:      fmt.Sprintf("CUST-%s", customerID[:8]),
		TaxNumber: "",
		KYCStatus: user.KYCState,
		Addresses: []models.CustomerDeliveryAddress{},
		Accounts:  []models.Account{},
		CreatedAt: time.Now(),
	}

	accountProductCode := req.Account.ProductCode
	if accountProductCode == "" {
		accountProductCode = "PWSR_DEBP_2404"
	}
	cardProductCode := req.Account.Card.ProductCode
	if cardProductCode == "" {
		cardProductCode = accountProductCode
	}

	account := models.Account{
		ID:               &accountID,
		SourceID:         accountID,
		CustomerID:       &customerID,
		CustomerSourceID: userID,
		ProductCode:      accountProductCode,
		Currency:         currency,
		AccountNumber:    generateAccountNumber(),
		Type:             "DEBIT",
		Status:           "ACTIVE",
		StatusReasonCode: "",
		Cards:            []models.Card{},
		CreatedAt:        time.Now(),
	}

	card := models.Card{
		ID:                 cardID,
		SourceID:           cardID,
		AccountID:          accountID,
		AccountSourceID:    accountID,
		CustomerID:         customerID,
		CustomerSourceID:   userID,
		NameOnCard:         req.NameOnCard,
		ProductCode:        cardProductCode,
		PanToken:           fmt.Sprintf("pan_%s", cardID),
		MaskedPan:          generateMaskedPan(),
		Status:             consts.CardStatusActive,
		StatusReasonCode:   nil,
		LockLevel:          nil,
		ExpiryDate:         time.Now().AddDate(3, 0, 0).Format("2006-01-02"),
		RelationType:       "PRIMARY",
		IsFirstTimeLock:    false,
		PlasticCreated:     false,
		OrderPlasticOnSync: false,
		CreatedAt:          time.Now(),
	}

	account.Cards = append(account.Cards, card)
	customer.Accounts = append(customer.Accounts, account)

	if err := h.store.CreateCustomer(&customer); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create customer")
		return
	}
	if err := h.store.CreateAccount(&account); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	if err := h.store.CreateCard(&card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create card")
		return
	}

	if req.Delivery != nil {
		address := models.CustomerDeliveryAddress{
			ID:               utils.GenerateUUID(),
			SourceID:         customerID,
			CustomerID:       customerID,
			CustomerSourceID: userID,
			Type:             req.Delivery.Type,
			Line1:            req.Delivery.Line1,
			Line2:            req.Delivery.Line2,
			Line3:            req.Delivery.Line3,
			City:             req.Delivery.City,
			PostOffice:       req.Delivery.PostOffice,
			ZipCode:          req.Delivery.ZipCode,
			CountryCode:      req.Delivery.CountryCode,
			Status:           "ACTIVE",
		}

		if err := h.store.CreateCustomerAddress(customerID, &address); err != nil {
			h.sendError(w, http.StatusInternalServerError, "failed to create delivery address")
			return
		}
		customer.Addresses = append(customer.Addresses, address)
		_ = h.store.UpdateCustomer(&customer)
	}

	response := models.CustomerResponse{
		WalletAddress: req.WalletAddress,
		Customer:      customer,
	}

	h.sendJSON(w, http.StatusCreated, response)

	go h.webhookManager.SendAsync(consts.WebhookEventCardCreated, userID, map[string]interface{}{
		"cardId":           card.ID,
		"cardSourceId":     card.SourceID,
		"nameOnCard":       card.NameOnCard,
		"productCode":      card.ProductCode,
		"maskedPan":        card.MaskedPan,
		"accountId":        accountID,
		"accountSourceId":  account.SourceID,
		"lockLevel":        card.LockLevel,
		"customerId":       customerID,
		"customerSourceId": customer.SourceID,
	})
}

// GetDeliveryAddresses lists all delivery addresses for a customer
func (h *Handler) GetDeliveryAddresses(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	if customerID == "" {
		h.sendError(w, http.StatusBadRequest, "customer ID is required")
		return
	}

	if _, err := h.store.GetCustomer(customerID); err != nil {
		h.sendError(w, http.StatusNotFound, "customer not found")
		return
	}

	addresses, err := h.store.GetCustomerAddresses(customerID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get addresses")
		return
	}

	response := make([]models.CustomerDeliveryAddress, 0, len(addresses))
	for _, addr := range addresses {
		response = append(response, *addr)
	}

	h.sendJSON(w, http.StatusOK, response)
}

// CreateCustomerDeliveryAddress adds a delivery address to a customer
func (h *Handler) CreateCustomerDeliveryAddress(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	if customerID == "" {
		h.sendError(w, http.StatusBadRequest, "customer ID is required")
		return
	}

	customer, err := h.store.GetCustomer(customerID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "customer not found")
		return
	}

	var req models.CreateCustomerDeliveryAddressArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateDeliveryAddress(req); err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	address := models.CustomerDeliveryAddress{
		ID:               utils.GenerateUUID(),
		SourceID:         customerID,
		CustomerID:       customerID,
		CustomerSourceID: customer.SourceID,
		Type:             req.Type,
		Line1:            req.Line1,
		Line2:            req.Line2,
		Line3:            req.Line3,
		City:             req.City,
		PostOffice:       req.PostOffice,
		ZipCode:          req.ZipCode,
		CountryCode:      req.CountryCode,
		Status:           "ACTIVE",
	}

	if err := h.store.CreateCustomerAddress(customerID, &address); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create address")
		return
	}

	addresses, err := h.store.GetCustomerAddresses(customerID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get addresses")
		return
	}

	response := make([]models.CustomerDeliveryAddress, 0, len(addresses))
	for _, addr := range addresses {
		response = append(response, *addr)
	}

	h.sendJSON(w, http.StatusCreated, response)
}

// OrderCard creates an additional card for an account
func (h *Handler) OrderCard(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	if accountID == "" {
		h.sendError(w, http.StatusBadRequest, "account ID is required")
		return
	}

	account, err := h.store.GetAccount(accountID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	var req models.OrderCardArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.NameOnCard == "" {
		h.sendError(w, http.StatusBadRequest, "nameOnCard is required")
		return
	}
	if len(req.NameOnCard) > 26 {
		h.sendError(w, http.StatusBadRequest, "nameOnCard exceeds 26 characters")
		return
	}

	currency := req.Currency
	if currency == "" {
		currency = "EUR"
	}
	if currency != "EUR" {
		h.sendError(w, http.StatusBadRequest, "only EUR currency is supported")
		return
	}

	cardProductCode := req.Card.ProductCode
	if cardProductCode == "" {
		cardProductCode = req.ProductCode
	}
	if cardProductCode == "" {
		cardProductCode = account.ProductCode
	}

	if req.DeliveryAddressID != nil {
		if account.CustomerID == nil || *account.CustomerID == "" {
			h.sendError(w, http.StatusBadRequest, "deliveryAddressId requires a customer")
			return
		}
		addresses, err := h.store.GetCustomerAddresses(*account.CustomerID)
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, "failed to validate delivery address")
			return
		}
		found := false
		for _, addr := range addresses {
			if addr.ID == *req.DeliveryAddressID {
				found = true
				break
			}
		}
		if !found {
			h.sendError(w, http.StatusBadRequest, "deliveryAddressId not found")
			return
		}
	}

	cardID := utils.GenerateUUID()
	card := models.Card{
		ID:                 cardID,
		SourceID:           cardID,
		AccountID:          accountID,
		AccountSourceID:    accountID,
		CustomerID:         derefString(account.CustomerID),
		CustomerSourceID:   account.CustomerSourceID,
		NameOnCard:         req.NameOnCard,
		ProductCode:        cardProductCode,
		PanToken:           fmt.Sprintf("pan_%s", cardID),
		MaskedPan:          generateMaskedPan(),
		Status:             consts.CardStatusActive,
		ExpiryDate:         time.Now().AddDate(3, 0, 0).Format("2006-01-02"),
		RelationType:       "SECONDARY",
		IsFirstTimeLock:    false,
		PlasticCreated:     false,
		OrderPlasticOnSync: false,
		CreatedAt:          time.Now(),
	}

	if err := h.store.CreateCard(&card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create card")
		return
	}

	account.Cards = append(account.Cards, card)
	_ = h.store.UpdateAccount(account)

	h.sendJSON(w, http.StatusOK, card)

	if account.CustomerSourceID != "" {
		go h.webhookManager.SendAsync(consts.WebhookEventCardCreated, account.CustomerSourceID, map[string]interface{}{
			"cardId":           card.ID,
			"cardSourceId":     card.SourceID,
			"nameOnCard":       card.NameOnCard,
			"productCode":      card.ProductCode,
			"maskedPan":        card.MaskedPan,
			"accountId":        accountID,
			"accountSourceId":  account.SourceID,
			"lockLevel":        card.LockLevel,
			"customerId":       derefString(account.CustomerID),
			"customerSourceId": account.CustomerSourceID,
		})
	}
}

// ListCards lists cards for a customer
func (h *Handler) ListCards(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	if customerID == "" {
		h.sendError(w, http.StatusBadRequest, "customer ID is required")
		return
	}

	if _, err := h.store.GetCustomer(customerID); err != nil {
		h.sendError(w, http.StatusNotFound, "customer not found")
		return
	}

	cards, err := h.store.GetCardsByCustomer(customerID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to list cards")
		return
	}

	var active []models.Card
	for _, card := range cards {
		if card.Status != consts.CardStatusSoftDelete {
			active = append(active, *card)
		}
	}

	pageSize := uint(100)
	if sizeStr := r.URL.Query().Get("pageSize"); sizeStr != "" {
		if size, err := strconv.Atoi(sizeStr); err == nil && size > 0 {
			pageSize = uint(size)
		}
	}

	response := models.ListCardsResponse{
		Data: active,
		Pagination: models.Pagination{
			PageNumber: 1,
			PageSize:   pageSize,
			TotalPages: 1,
		},
	}

	h.sendJSON(w, http.StatusOK, response)
}

// GetCard retrieves card details
func (h *Handler) GetCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// GetCardLimits returns card limits
func (h *Handler) GetCardLimits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	if _, err := h.store.GetCard(cardID); err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	limits, err := h.store.GetCardLimits(cardID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get card limits")
		return
	}

	if len(limits) == 0 {
		limits = defaultCardLimits()
	}

	h.sendJSON(w, http.StatusOK, limits)
}

// SetCardLimits sets card limits
func (h *Handler) SetCardLimits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	if _, err := h.store.GetCard(cardID); err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	var req []models.CardLimit
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for i := range req {
		if req[i].Type == "" {
			h.sendError(w, http.StatusBadRequest, "limit type is required")
			return
		}
		if req[i].Currency == "" {
			req[i].Currency = "EUR"
		}
		if req[i].Currency != "EUR" {
			h.sendError(w, http.StatusBadRequest, "only EUR currency is supported")
			return
		}
		if req[i].Limit < 0 {
			h.sendError(w, http.StatusBadRequest, "limit must be non-negative")
			return
		}
	}

	if err := h.store.SetCardLimits(cardID, req); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to set card limits")
		return
	}

	h.sendJSON(w, http.StatusCreated, req)
}

// LockCard locks a card temporarily
func (h *Handler) LockCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	reasonCode := r.URL.Query().Get("reasonCode")
	if reasonCode == "" {
		h.sendError(w, http.StatusBadRequest, "reasonCode is required")
		return
	}

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if card.Status == consts.CardStatusSoftDelete || card.Status == consts.CardStatusBlocked {
		h.sendError(w, http.StatusBadRequest, "card cannot be locked")
		return
	}
	if card.Status == consts.CardStatusTemporaryBlocked {
		h.sendError(w, http.StatusBadRequest, "card already locked")
		return
	}

	card.Status = consts.CardStatusTemporaryBlocked
	card.LockLevel = &reasonCode
	if !card.IsFirstTimeLock {
		card.IsFirstTimeLock = true
	}

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// UnlockCard unlocks a previously locked card
func (h *Handler) UnlockCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if card.Status != consts.CardStatusTemporaryBlocked {
		h.sendError(w, http.StatusBadRequest, "card is not locked")
		return
	}

	card.Status = consts.CardStatusActive
	card.LockLevel = nil

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// BlockCard permanently blocks a card
func (h *Handler) BlockCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	reasonCode := r.URL.Query().Get("reasonCode")
	if reasonCode == "" {
		h.sendError(w, http.StatusBadRequest, "reasonCode is required")
		return
	}

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if card.Status == consts.CardStatusSoftDelete {
		h.sendError(w, http.StatusBadRequest, "card is closed")
		return
	}
	if card.Status == consts.CardStatusBlocked {
		h.sendError(w, http.StatusBadRequest, "card already blocked")
		return
	}

	card.Status = consts.CardStatusBlocked
	card.StatusReasonCode = &reasonCode
	card.LockLevel = nil

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// DeleteCard closes (soft-deletes) a card
func (h *Handler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if card.Status == consts.CardStatusSoftDelete {
		h.sendError(w, http.StatusBadRequest, "card already closed")
		return
	}

	card.Status = consts.CardStatusSoftDelete
	card.StatusReasonCode = nil
	card.LockLevel = nil

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GetCardToken returns a short-lived token for card data access
func (h *Handler) GetCardToken(w http.ResponseWriter, r *http.Request) {
	tokenType := chi.URLParam(r, "tokenType")
	if tokenType == "" {
		h.sendError(w, http.StatusBadRequest, "tokenType is required")
		return
	}
	if tokenType != "card-data" && tokenType != "pin" && tokenType != "pin-change" {
		h.sendError(w, http.StatusBadRequest, "unsupported tokenType")
		return
	}

	var req models.GetCardTokenArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CardID == "" {
		h.sendError(w, http.StatusBadRequest, "cardId is required")
		return
	}

	if _, err := h.store.GetCard(req.CardID); err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	token := fmt.Sprintf("mock-%s-%s-%s", tokenType, req.CardID, utils.GenerateUUID())
	response := models.CardTokenResponse{Token: token}
	if tokenType == "pin-change" {
		response.Links = []models.CardTokenLink{
			{
				Href:   "/cards/v1/pin/change",
				Rel:    "pin-change",
				Method: http.MethodPost,
			},
		}
	} else {
		response.Links = []models.CardTokenLink{
			{
				Href:   fmt.Sprintf("/cards/v1/token/%s/data?token=%s", tokenType, token),
				Rel:    "data",
				Method: http.MethodGet,
			},
		}
	}

	h.sendJSON(w, http.StatusOK, response)
}

// GetTokenData returns tokenized card data or pin cipher
func (h *Handler) GetTokenData(w http.ResponseWriter, r *http.Request) {
	tokenType := chi.URLParam(r, "tokenType")
	if tokenType == "" {
		h.sendError(w, http.StatusBadRequest, "tokenType is required")
		return
	}
	if tokenType != "card-data" && tokenType != "pin" {
		h.sendError(w, http.StatusBadRequest, "unsupported tokenType")
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		h.sendError(w, http.StatusBadRequest, "token is required")
		return
	}

	cardID, err := parseCardToken(token, tokenType)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid token")
		return
	}

	if _, err := h.store.GetCard(cardID); err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if tokenType == "card-data" {
		h.sendJSON(w, http.StatusOK, map[string]string{"cipher": "mock-encrypted-card-data"})
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]string{"cipher": "mock-encrypted-pin"})
}

// ChangePin accepts a tokenized pin change request
func (h *Handler) ChangePin(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		h.sendError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	cardID, err := parseCardToken(token, "pin-change")
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	if _, err := h.store.GetCard(cardID); err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	var req models.ChangePinArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Cypher == "" {
		h.sendError(w, http.StatusBadRequest, "cypher is required")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateCardTransaction creates a mock card transaction and emits a webhook
func (h *Handler) CreateCardTransaction(w http.ResponseWriter, r *http.Request) {
	var req models.CreateCardTransactionArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CardID == "" {
		h.sendError(w, http.StatusBadRequest, "cardId is required")
		return
	}
	if req.Amount == "" {
		h.sendError(w, http.StatusBadRequest, "amount is required")
		return
	}
	if req.Currency == "" {
		req.Currency = "EUR"
	}

	card, err := h.store.GetCard(req.CardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	txID := utils.GenerateUUID()
	amount := req.Amount
	currency := req.Currency
	now := time.Now().UTC()
	created := now.Format(time.RFC3339)

	txStatus := "COMPLETED"
	merchantName := req.MerchantName
	merchantCity := req.MerchantCity
	merchantCountry := req.MerchantCountry
	classification := "Advice"

	transaction := models.CardTransaction{
		TransactionID:             txID,
		GHResponseCode:            "00",
		GHResponseDescription:     "Approved",
		TransactionAmount:         &amount,
		TransactionCurrency:       &currency,
		BillingAmount:             &amount,
		BillingCurrency:           &currency,
		IsTrxAmountConverted:      false,
		TerminalID:                "MOCKTERM",
		CardScheme:                2,
		Type:                      req.Type,
		CreatedAt:                 created,
		TxStatus:                  &txStatus,
		Operation:                 0,
		MerchantName:              merchantName,
		MerchantCity:              merchantCity,
		MerchantCountry:           merchantCountry,
		TransactionClassification: &classification,
	}

	if err := h.store.CreateCardTransaction(&transaction); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to store card transaction")
		return
	}
	if err := h.store.AddCardTransactionIndex(card.ID, txID); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to index card transaction")
		return
	}

	h.sendJSON(w, http.StatusCreated, transaction)

	go h.webhookManager.SendAsync(consts.WebhookEventCardTransaction, card.CustomerSourceID, map[string]interface{}{
		"title":         "Card transaction",
		"body":          fmt.Sprintf("%s %s", amount, currency),
		"transactionId": txID,
		"cardId":        card.ID,
	})
}

// GetCardTransaction returns a stored card transaction by ID
func (h *Handler) GetCardTransaction(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	if txID == "" {
		h.sendError(w, http.StatusBadRequest, "transaction ID is required")
		return
	}

	tx, err := h.store.GetCardTransaction(txID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card transaction not found")
		return
	}

	h.sendJSON(w, http.StatusOK, tx)
}

// GetCardTransactions lists card transactions with pagination
func (h *Handler) GetCardTransactions(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "card ID is required")
		return
	}

	if _, err := h.store.GetCard(cardID); err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	pageSize := uint(20)
	if sizeStr := r.URL.Query().Get("pageSize"); sizeStr != "" {
		if size, err := strconv.Atoi(sizeStr); err == nil && size > 0 {
			pageSize = uint(size)
		}
	}
	pageNumber := uint(1)
	if pageStr := r.URL.Query().Get("pageNumber"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			pageNumber = uint(page)
		}
	}

	ids, err := h.store.GetCardTransactionIDs(cardID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get card transactions")
		return
	}

	totalRecords := uint(len(ids))
	if totalRecords == 0 {
		response := models.CardTransactionsResponse{
			Data: []models.CardTransaction{},
			Pagination: models.CardTransactionsPagination{
				PageNumber:   pageNumber,
				PageSize:     pageSize,
				TotalPages:   0,
				TotalRecords: 0,
			},
		}
		h.sendJSON(w, http.StatusOK, response)
		return
	}

	start := (pageNumber - 1) * pageSize
	if start >= totalRecords {
		start = totalRecords
	}
	end := start + pageSize
	if end > totalRecords {
		end = totalRecords
	}

	pageIDs := ids[start:end]
	transactions := make([]models.CardTransaction, 0, len(pageIDs))
	for _, id := range pageIDs {
		if tx, err := h.store.GetCardTransaction(id); err == nil {
			transactions = append(transactions, *tx)
		}
	}

	totalPages := (totalRecords + pageSize - 1) / pageSize
	response := models.CardTransactionsResponse{
		Data: transactions,
		Pagination: models.CardTransactionsPagination{
			PageNumber:   pageNumber,
			PageSize:     pageSize,
			TotalPages:   totalPages,
			TotalRecords: totalRecords,
		},
	}

	h.sendJSON(w, http.StatusOK, response)
}

// GetPendingConfirmations returns pending 3DS challenges for the authenticated user
func (h *Handler) GetPendingConfirmations(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get(managedUserHeader)
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "x-gatehub-managed-user-uuid header is required")
		return
	}

	challenges, err := h.store.GetPendingThreeDSChallenges(userID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get pending confirmations")
		return
	}

	// Convert to API format
	var confirmations []models.PendingThreeDSConfirmation
	for _, challenge := range challenges {
		confirmations = append(confirmations, models.PendingThreeDSConfirmation{
			TransactionID:    challenge.TransactionID,
			MerchantName:     challenge.MerchantName,
			PurchaseAmount:   challenge.PurchaseAmount,
			PurchaseCurrency: challenge.PurchaseCurrency,
			PurchaseDate:     challenge.PurchaseDate,
			Timeout:          challenge.Timeout.Format(time.RFC3339),
		})
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"pendingConfirmations": confirmations,
	})
}

// ThreeDSPaymentConfirmation confirms or declines a 3DS payment
func (h *Handler) ThreeDSPaymentConfirmation(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	if txID == "" {
		h.sendError(w, http.StatusBadRequest, "transaction ID is required")
		return
	}

	userID := r.Header.Get(managedUserHeader)
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "x-gatehub-managed-user-uuid header is required")
		return
	}

	var req models.ThreeDSPaymentConfirmationArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AuthMethod == "" {
		h.sendError(w, http.StatusBadRequest, "authMethod is required")
		return
	}

	// Get the challenge
	challenge, err := h.store.GetThreeDSChallenge(txID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "3DS challenge not found")
		return
	}

	// Verify ownership
	if challenge.UserID != userID {
		h.sendError(w, http.StatusForbidden, "not authorized")
		return
	}

	// Check if not already resolved
	if challenge.Status != "pending" {
		h.sendError(w, http.StatusBadRequest, "challenge already resolved")
		return
	}

	// Check if not expired
	if time.Now().After(challenge.Timeout) {
		challenge.Status = "expired"
		_ = h.store.UpdateThreeDSChallenge(challenge)
		h.sendError(w, http.StatusGone, "challenge expired")
		return
	}

	// Update status based on confirmation
	if req.Confirmed {
		challenge.Status = "approved"
	} else {
		challenge.Status = "declined"
	}

	if err := h.store.UpdateThreeDSChallenge(challenge); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update challenge")
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  challenge.Status,
	})
}

// CreateThreeDSChallenge creates a mock 3DS challenge for testing
func (h *Handler) CreateThreeDSChallenge(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get(managedUserHeader)
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "x-gatehub-managed-user-uuid header is required")
		return
	}

	var req struct {
		CardID           string `json:"cardId"`
		MerchantName     string `json:"merchantName"`
		PurchaseAmount   string `json:"purchaseAmount"`
		PurchaseCurrency string `json:"purchaseCurrency"`
		TimeoutMinutes   int    `json:"timeoutMinutes"` // Default: 5 minutes
	}

	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CardID == "" {
		h.sendError(w, http.StatusBadRequest, "cardId is required")
		return
	}

	card, err := h.store.GetCard(req.CardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	// Set defaults
	if req.MerchantName == "" {
		req.MerchantName = "Test Merchant"
	}
	if req.PurchaseAmount == "" {
		req.PurchaseAmount = "100.00"
	}
	if req.PurchaseCurrency == "" {
		req.PurchaseCurrency = "EUR"
	}
	if req.TimeoutMinutes == 0 {
		req.TimeoutMinutes = 5
	}

	now := time.Now()
	timeout := now.Add(time.Duration(req.TimeoutMinutes) * time.Minute)
	txID := utils.GenerateUUID()

	challenge := models.ThreeDSChallenge{
		TransactionID:    txID,
		CardID:           card.ID,
		UserID:           userID,
		MerchantName:     req.MerchantName,
		PurchaseAmount:   req.PurchaseAmount,
		PurchaseCurrency: req.PurchaseCurrency,
		PurchaseDate:     now.Format(time.RFC3339),
		Timeout:          timeout,
		Status:           "pending",
		CreatedAt:        now,
	}

	if err := h.store.CreateThreeDSChallenge(&challenge); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create challenge")
		return
	}

	h.sendJSON(w, http.StatusCreated, challenge)

	// Send webhook
	go h.webhookManager.SendAsync(consts.WebhookEventCard3DS, userID, map[string]interface{}{
		"type": "3ds_challenge",
		"payload": map[string]interface{}{
			"transactionId":    challenge.TransactionID,
			"merchantName":     challenge.MerchantName,
			"purchaseAmount":   challenge.PurchaseAmount,
			"purchaseCurrency": challenge.PurchaseCurrency,
			"purchaseDate":     challenge.PurchaseDate,
			"timeout":          challenge.Timeout.Format(time.RFC3339),
		},
	})
}

func generateMaskedPan() string {
	last4 := make([]byte, 4)
	_, _ = rand.Read(last4)
	for i := 0; i < len(last4); i++ {
		last4[i] = '0' + (last4[i] % 10)
	}
	return fmt.Sprintf("512345******%s", string(last4))
}

func generateAccountNumber() string {
	buf := make([]byte, 10)
	_, _ = rand.Read(buf)
	for i := 0; i < len(buf); i++ {
		buf[i] = '0' + (buf[i] % 10)
	}
	return fmt.Sprintf("GH%s", string(buf))
}

func validateDeliveryAddress(req models.CreateCustomerDeliveryAddressArgs) error {
	if req.Type == "" {
		return fmt.Errorf("type is required")
	}
	if req.CountryCode == "" || len(req.CountryCode) != 3 {
		return fmt.Errorf("countryCode must be ISO alpha-3")
	}
	if req.Line1 == "" || len(req.Line1) > 128 {
		return fmt.Errorf("line1 must be 1-128 characters")
	}
	if req.Line2 != nil && len(*req.Line2) > 128 {
		return fmt.Errorf("line2 must be 1-128 characters")
	}
	if req.Line3 != nil && len(*req.Line3) > 128 {
		return fmt.Errorf("line3 must be 1-128 characters")
	}
	if req.City == "" || len(req.City) > 32 {
		return fmt.Errorf("city must be 1-32 characters")
	}
	if req.PostOffice != nil && len(*req.PostOffice) > 32 {
		return fmt.Errorf("postOffice must be <= 32 characters")
	}
	if req.ZipCode == "" || len(req.ZipCode) > 8 {
		return fmt.Errorf("zipCode must be 1-8 characters")
	}
	if req.Reason == "" || len(req.Reason) > 254 {
		return fmt.Errorf("reason must be 1-254 characters")
	}

	return nil
}

func defaultCardLimits() []models.CardLimit {
	return []models.CardLimit{
		{Type: "dailyOverall", Limit: 1000.00, Currency: "EUR", IsDisabled: false},
		{Type: "perTransaction", Limit: 500.00, Currency: "EUR", IsDisabled: false},
		{Type: "monthlyOverall", Limit: 5000.00, Currency: "EUR", IsDisabled: false},
		{Type: "dailyAtm", Limit: 300.00, Currency: "EUR", IsDisabled: false},
		{Type: "dailyEcomm", Limit: 800.00, Currency: "EUR", IsDisabled: false},
	}
}

func parseCardToken(token string, tokenType string) (string, error) {
	prefix := fmt.Sprintf("mock-%s-", tokenType)
	if !strings.HasPrefix(token, prefix) {
		return "", fmt.Errorf("invalid token")
	}

	remainder := strings.TrimPrefix(token, prefix)
	if len(remainder) <= 37 {
		return "", fmt.Errorf("invalid token")
	}
	sep := len(remainder) - 37
	if remainder[sep] != '-' {
		return "", fmt.Errorf("invalid token")
	}

	cardID := remainder[:sep]
	if cardID == "" {
		return "", fmt.Errorf("invalid token")
	}

	return cardID, nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// GetCardApplicationProducts returns available card products for a given application
// Endpoint: GET /cards/v1/card-applications/{appID}/card-products
func (h *Handler) GetCardApplicationProducts(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	if appID == "" {
		h.sendError(w, http.StatusBadRequest, "appID is required")
		return
	}

	// Hardcoded card products response (sandbox simulation)
	products := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"id":          "PROD_VIRTUAL_CARD",
				"code":        "PROD_VIRTUAL_CARD",
				"name":        "Virtual Card EUR",
				"description": "Virtual Mastercard for online purchases in EUR",
				"type":        "VIRTUAL",
				"currency":    "EUR",
				"active":      true,
			},
			{
				"id":          "PROD_PLASTIC_CARD",
				"code":        "PROD_PLASTIC_CARD",
				"name":        "Plastic Card EUR",
				"description": "Physical Mastercard for in-store and online purchases in EUR",
				"type":        "PLASTIC",
				"currency":    "EUR",
				"active":      true,
			},
		},
		"pagination": map[string]interface{}{
			"pageNumber": 1,
			"pageSize":   10,
			"totalPages": 1,
		},
	}

	h.sendJSON(w, http.StatusOK, products)
}

// CreatePlasticForCard creates a physical card for an existing card
// Endpoint: POST /cards/v1/cards/{cardID}/plastic
// This is a stub that always returns success (plastic cards are not physically shipped in mock)
func (h *Handler) CreatePlasticForCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if cardID == "" {
		h.sendError(w, http.StatusBadRequest, "cardID is required")
		return
	}

	// Verify card exists
	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	// Update card to mark plastic as created
	card.PlasticCreated = true
	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	// Return success response with plastic order details
	response := map[string]interface{}{
		"message":       "Plastic card order created successfully",
		"orderId":       utils.GenerateUUID(),
		"cardId":        cardID,
		"status":        "PENDING",
		"type":          "PLASTIC",
		"estimatedDate": time.Now().AddDate(0, 0, 7).Format("2006-01-02"),
		"deliveryAddress": map[string]interface{}{
			"firstName":    "Mock",
			"lastName":     "User",
			"addressLine1": "123 Mock Street",
			"city":         "Mock City",
			"zipCode":      "12345",
			"country":      "Mock Country",
		},
	}

	h.sendJSON(w, http.StatusCreated, response)
}

// NOTE: real implementation exists above; stub removed
