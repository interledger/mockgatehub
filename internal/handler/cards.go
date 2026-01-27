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

	card.Status = consts.CardStatusSoftDelete
	card.StatusReasonCode = nil
	card.LockLevel = nil

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]bool{"success": true})
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
