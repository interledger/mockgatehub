package handler

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
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
	response := models.CardTokenResponse{
		Token: token,
		Links: []models.CardTokenLink{
			{
				Href:   fmt.Sprintf("/cards/v1/token/%s/data?token=%s", tokenType, token),
				Rel:    "data",
				Method: http.MethodGet,
			},
		},
	}

	h.sendJSON(w, http.StatusOK, response)
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

// GetPendingConfirmations returns an empty 3DS confirmation list (stub for now).
func (h *Handler) GetPendingConfirmations(w http.ResponseWriter, r *http.Request) {
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"pendingConfirmations": []interface{}{},
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

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
