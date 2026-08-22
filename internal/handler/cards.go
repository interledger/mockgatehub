package handler

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"
	"mockgatehub/internal/utils"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const (
	// defaultCardTxPageSize matches what the previous implementation reported.
	defaultCardTxPageSize = 20
	// maxCardTxPageSize caps a caller-supplied page size so one request cannot
	// ask for an unbounded response.
	maxCardTxPageSize = 200
)

// defaultCardLimits returns the spending limits a new card starts with.
func defaultCardLimits() []models.CardLimit {
	return []models.CardLimit{
		{Type: "dailyOverall", Limit: 1000.00, Currency: "EUR", IsDisabled: false},
		{Type: "perTransaction", Limit: 500.00, Currency: "EUR", IsDisabled: false},
		{Type: "monthlyOverall", Limit: 5000.00, Currency: "EUR", IsDisabled: false},
		{Type: "dailyAtm", Limit: 300.00, Currency: "EUR", IsDisabled: false},
		{Type: "dailyEcomm", Limit: 800.00, Currency: "EUR", IsDisabled: false},
	}
}

// generateMaskedPan generates a realistic masked PAN with numeric-only digits
func generateMaskedPan() string {
	return fmt.Sprintf("%s******%s", randomDigits(6), randomDigits(4))
}

// randomDigits returns a string of n cryptographically random decimal digits
func randomDigits(n int) string {
	digits := make([]byte, n)
	for i := range digits {
		num, _ := rand.Int(rand.Reader, big.NewInt(10))
		digits[i] = '0' + byte(num.Int64())
	}
	return string(digits)
}

// CreateCustomer creates a card customer (generic endpoint)
func (h *Handler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	h.CreateManagedCustomer(w, r)
}

// CreateManagedCustomer creates a card customer with account and initial card
func (h *Handler) CreateManagedCustomer(w http.ResponseWriter, r *http.Request) {
	logger.Info("create managed customer called")

	userID := r.Header.Get("x-gatehub-managed-user-uuid")
	if userID == "" {
		h.sendError(w, http.StatusBadRequest, "missing x-gatehub-managed-user-uuid header")
		return
	}

	// Validate user exists and KYC is accepted
	user, err := h.store.GetUser(userID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "user not found in GateHub")
		return
	}

	logger.Info("card creation requested",
		zap.String("user_id", userID),
		zap.String("current_kyc_state", user.KYCState),
		zap.Bool("kyc_accepted", user.KYCState == consts.KYCStateAccepted))

	if user.KYCState != consts.KYCStateAccepted {
		h.sendError(w, http.StatusForbidden, fmt.Sprintf("user KYC state is '%s' but must be 'accepted' before ordering cards", user.KYCState))
		return
	}

	var req models.CreateCustomerAndCardArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate nameOnCard
	if req.NameOnCard == "" {
		h.sendError(w, http.StatusBadRequest, "nameOnCard is required")
		return
	}
	if len(req.NameOnCard) > 26 {
		h.sendError(w, http.StatusBadRequest, fmt.Sprintf("nameOnCard must be 26 characters or less (provided: %d characters)", len(req.NameOnCard)))
		return
	}

	// Validate currency - accept both EUR and prefixed variants like PW_EUR, DEB_EUR
	currency := req.Account.Currency
	if currency == "" {
		currency = "EUR"
	}
	// Strip any prefix (PW_, DEB_, etc.) and validate base currency is EUR
	baseCurrency := currency
	if idx := strings.LastIndex(currency, "_"); idx >= 0 {
		baseCurrency = currency[idx+1:]
	}
	if baseCurrency != "EUR" {
		h.sendError(w, http.StatusBadRequest, fmt.Sprintf("invalid currency '%s': only EUR-based currencies are supported for cards (e.g., 'EUR', 'PW_EUR', 'DEB_EUR')", currency))
		return
	}

	logger.Debug("customer creation request validated", zap.String("user_id", userID), zap.String("currency", currency), zap.String("name_on_card", req.NameOnCard))

	productCode := req.Account.ProductCode
	if productCode == "" {
		productCode = consts.DefaultCardProductCode
	}

	// Create customer
	customerID := utils.GenerateUUID()
	customer := &models.Customer{
		ID:        &customerID,
		SourceID:  userID,
		Type:      consts.DefaultCustomerType,
		Code:      fmt.Sprintf("CUST-%s", customerID[:8]),
		KYCStatus: consts.KYCStateAccepted,
		CreatedAt: time.Now(),
	}

	if err := h.store.CreateCustomer(customer); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create customer")
		return
	}

	// Create delivery address if provided
	if req.Delivery != nil {
		addr := &models.CustomerDeliveryAddress{
			ID:               utils.GenerateUUID(),
			SourceID:         utils.GenerateUUID(),
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
			Status:           consts.DefaultStatusActive,
		}
		if err := h.store.CreateCustomerAddress(customerID, addr); err != nil {
			logger.Warn("failed to create delivery address", zap.Error(err))
		}
	}

	// Create account
	accountID := utils.GenerateUUID()
	account := &models.Account{
		ID:               &accountID,
		SourceID:         accountID,
		CustomerID:       &customerID,
		CustomerSourceID: userID,
		ProductCode:      productCode,
		Currency:         currency,
		AccountNumber:    fmt.Sprintf("GB29NWBK%s", utils.GenerateUUID()[:12]),
		Type:             consts.DefaultAccountType,
		Status:           consts.DefaultStatusActive,
		CreatedAt:        time.Now(),
	}

	if err := h.store.CreateAccount(account); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	// Create card
	cardID := utils.GenerateUUID()
	card := &models.Card{
		ID:               cardID,
		SourceID:         cardID,
		AccountID:        accountID,
		AccountSourceID:  accountID,
		CustomerID:       customerID,
		CustomerSourceID: userID,
		NameOnCard:       req.NameOnCard,
		ProductCode:      productCode,
		PanToken:         fmt.Sprintf("pan_%s", cardID),
		MaskedPan:        generateMaskedPan(),
		Status:           consts.CardStatusActive,
		ExpiryDate:       time.Now().AddDate(3, 0, 0).Format("2006-01-02"),
		RelationType:     consts.CardRelationPrimary,
		IsFirstTimeLock:  false,
		PlasticCreated:   false,
		CreatedAt:        time.Now(),
	}

	if err := h.store.CreateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create card")
		return
	}

	// Seed default card limits
	if err := h.store.SetCardLimits(cardID, defaultCardLimits()); err != nil {
		logger.Warn("failed to seed card limits", zap.Error(err))
	}

	// Build response matching the documented CustomerResponse shape
	account.Cards = []models.Card{*card}
	customer.Accounts = []models.Account{*account}

	response := models.CustomerResponse{
		WalletAddress: req.WalletAddress,
		Customer:      *customer,
	}

	h.sendJSON(w, http.StatusCreated, response)

	// Tell the consumer which customer, account and card ids were assigned.
	// Without this they would have to poll to discover them.
	h.webhookManager.SendAsync(consts.WebhookEventCardCreated, userID, map[string]interface{}{
		"cardId":           cardID,
		"cardSourceId":     cardID,
		"nameOnCard":       req.NameOnCard,
		"productCode":      productCode,
		"maskedPan":        card.MaskedPan,
		"accountId":        accountID,
		"accountSourceId":  accountID,
		"lockLevel":        nil,
		"customerId":       customerID,
		"customerSourceId": userID,
	}, 0)
}

// ListCards retrieves cards for a customer
func (h *Handler) ListCards(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	logger.Info("list cards called", zap.String("customer_id", customerID))

	cards, err := h.store.GetCardsByCustomer(customerID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get cards")
		return
	}

	// Filter out SoftDelete cards
	var activeCards []models.Card
	for _, c := range cards {
		if c.Status != consts.CardStatusSoftDelete {
			activeCards = append(activeCards, *c)
		}
	}
	if activeCards == nil {
		activeCards = []models.Card{}
	}

	response := models.ListCardsResponse{
		Data: activeCards,
		Pagination: models.Pagination{
			PageNumber: 1,
			PageSize:   100,
			TotalPages: 1,
		},
	}

	h.sendJSON(w, http.StatusOK, response)
}

// CreateCard creates a new card (stub)
func (h *Handler) CreateCard(w http.ResponseWriter, r *http.Request) {
	logger.Info("create card called")
	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"id":     utils.GenerateUUID(),
		"status": consts.CardStatusActive,
	})
}

// GetCard retrieves card details
func (h *Handler) GetCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("get card called", zap.String("card_id", cardID))

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// DeleteCard soft-deletes a card
func (h *Handler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("delete card called", zap.String("card_id", cardID))

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if card.Status == consts.CardStatusSoftDelete {
		h.sendError(w, http.StatusBadRequest, "card is already deleted")
		return
	}

	card.Status = consts.CardStatusSoftDelete
	card.StatusReasonCode = nil
	card.LockLevel = nil

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to delete card")
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// LockCard locks a card temporarily
func (h *Handler) LockCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	reasonCode := r.URL.Query().Get("reasonCode")
	logger.Info("lock card called", zap.String("card_id", cardID), zap.String("reason_code", reasonCode))

	if reasonCode == "" {
		reasonCode = "ClientRequestedLock"
	}

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	// Validate card can be locked
	if card.Status == consts.CardStatusSoftDelete {
		h.sendError(w, http.StatusBadRequest, "cannot lock a deleted card")
		return
	}
	if card.Status == consts.CardStatusBlocked {
		h.sendError(w, http.StatusBadRequest, "cannot lock a permanently blocked card")
		return
	}
	if card.Status == consts.CardStatusTemporaryBlocked {
		h.sendError(w, http.StatusBadRequest, "card is already locked")
		return
	}

	card.Status = consts.CardStatusTemporaryBlocked
	card.LockLevel = &reasonCode
	if !card.IsFirstTimeLock {
		card.IsFirstTimeLock = true
	}

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to lock card")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// UnlockCard unlocks a temporarily blocked card
func (h *Handler) UnlockCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("unlock card called", zap.String("card_id", cardID))

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
		h.sendError(w, http.StatusInternalServerError, "failed to unlock card")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// BlockCard permanently blocks a card
func (h *Handler) BlockCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	reasonCode := r.URL.Query().Get("reasonCode")
	logger.Info("block card called", zap.String("card_id", cardID))

	card, err := h.store.GetCard(cardID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "card not found")
		return
	}

	if card.Status == consts.CardStatusSoftDelete {
		h.sendError(w, http.StatusBadRequest, "cannot block a deleted card")
		return
	}
	if card.Status == consts.CardStatusBlocked {
		h.sendError(w, http.StatusBadRequest, "card is already blocked")
		return
	}

	card.Status = consts.CardStatusBlocked
	card.StatusReasonCode = &reasonCode
	card.LockLevel = nil

	if err := h.store.UpdateCard(card); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to block card")
		return
	}

	h.sendJSON(w, http.StatusOK, card)
}

// GetPendingConfirmations retrieves pending 3DS confirmations
func (h *Handler) GetPendingConfirmations(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("x-gatehub-managed-user-uuid")
	logger.Info("get pending confirmations called", zap.String("user_id", userID))

	challenges, err := h.store.GetPendingThreeDSChallenges(userID)
	if err != nil {
		h.sendJSON(w, http.StatusOK, map[string]interface{}{"pendingConfirmations": []models.PendingThreeDSConfirmation{}})
		return
	}

	var pending []models.PendingThreeDSConfirmation
	for _, c := range challenges {
		remaining := int(time.Until(c.Timeout).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		pending = append(pending, models.PendingThreeDSConfirmation{
			TransactionID:    c.TransactionID,
			MerchantName:     c.MerchantName,
			PurchaseAmount:   c.PurchaseAmount,
			PurchaseCurrency: c.PurchaseCurrency,
			PurchaseDate:     c.PurchaseDate,
			Timeout:          fmt.Sprintf("%d", remaining),
		})
	}

	if pending == nil {
		pending = []models.PendingThreeDSConfirmation{}
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{"pendingConfirmations": pending})
}

// CreateCustomerAddress creates a delivery address for a card customer
func (h *Handler) CreateCustomerAddress(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	logger.Info("creating customer address", zap.String("customer_id", customerID))

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	address := &models.CustomerDeliveryAddress{
		ID:         utils.GenerateUUID(),
		CustomerID: customerID,
		Type:       fmt.Sprintf("%v", req["type"]),
		Line1:      fmt.Sprintf("%v", req["line1"]),
		City:       fmt.Sprintf("%v", req["city"]),
		ZipCode:    fmt.Sprintf("%v", req["zipCode"]),
		Status:     "ACTIVE",
	}
	if cc, ok := req["countryCode"].(string); ok {
		address.CountryCode = cc
	}

	if err := h.store.CreateCustomerAddress(customerID, address); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create address")
		return
	}

	h.sendJSON(w, http.StatusCreated, []models.CustomerDeliveryAddress{*address})
}

// GetCustomerAddresses retrieves delivery addresses for a card customer
func (h *Handler) GetCustomerAddresses(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerID")
	logger.Info("getting customer addresses", zap.String("customer_id", customerID))

	addresses, err := h.store.GetCustomerAddresses(customerID)
	if err != nil {
		h.sendJSON(w, http.StatusOK, []models.CustomerDeliveryAddress{})
		return
	}

	h.sendJSON(w, http.StatusOK, addresses)
}

// OrderAdditionalCard orders an additional card for an account
func (h *Handler) OrderAdditionalCard(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	logger.Info("ordering additional card", zap.String("account_id", accountID))

	card := map[string]interface{}{
		"id":         utils.GenerateUUID(),
		"accountID":  accountID,
		"status":     "PENDING",
		"cardNumber": "****1234",
		"createdAt":  time.Now().UTC().Format(time.RFC3339),
	}

	h.sendJSON(w, http.StatusCreated, card)
}

// GetCardLimits retrieves spending limits for a card
func (h *Handler) GetCardLimits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("get card limits called", zap.String("card_id", cardID))

	limits, err := h.store.GetCardLimits(cardID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get limits")
		return
	}

	// Seed default limits if empty
	if len(limits) == 0 {
		limits = defaultCardLimits()
		_ = h.store.SetCardLimits(cardID, limits)
	}

	h.sendJSON(w, http.StatusOK, limits)
}

// UpdateCardLimits updates spending limits for a card
func (h *Handler) UpdateCardLimits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("update card limits called", zap.String("card_id", cardID))

	var limits []models.CardLimit
	if err := json.NewDecoder(r.Body).Decode(&limits); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.store.SetCardLimits(cardID, limits); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update limits")
		return
	}

	h.sendJSON(w, http.StatusOK, limits)
}

// GetCardToken mints a short-lived token the browser exchanges for sensitive
// card data.
//
// The route keeps its {tokenType} wildcard: consumers call it as card-data,
// pin and pin-change, and narrowing it to one literal type would 404 the other
// two. Each type produces a token scoped to itself, so a token minted to read
// a PIN cannot be replayed against the change-PIN endpoint.
//
// The returned links[0].href is absolute, because the browser follows it
// directly rather than resolving it against this service's own origin.
func (h *Handler) GetCardToken(w http.ResponseWriter, r *http.Request) {
	tokenType := chi.URLParam(r, "tokenType")
	if tokenType == "" {
		tokenType = cardTokenTypeCardData
	}
	logger.Info("get card token called", zap.String("token_type", tokenType))

	var req models.GetCardTokenArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CardID == "" {
		h.sendError(w, http.StatusBadRequest, "cardId is required")
		return
	}

	var dataPath string
	switch tokenType {
	case cardTokenTypeCardData:
		dataPath = cardDataPath
	case cardTokenTypePin, cardTokenTypePinChange:
		dataPath = cardPinPath
	default:
		// Token types this service does not implement a data endpoint for
		// (wallet provisioning, for example) keep their long-standing
		// placeholder response. Refusing them would break callers that only
		// need a token-shaped value back.
		h.sendPlaceholderCardToken(w, tokenType, req.CardID)
		return
	}

	// card-data and pin return data encrypted to the caller, so they cannot
	// work without a key to encrypt to. Rejecting here keeps the token and the
	// data endpoint consistent rather than failing later with a token in hand.
	var publicKey string
	if tokenType != cardTokenTypePinChange {
		if req.PublicKey == nil || strings.TrimSpace(*req.PublicKey) == "" {
			h.sendError(w, http.StatusBadRequest,
				fmt.Sprintf("publicKey is required for a %s token: the response is encrypted with it", tokenType))
			return
		}
		publicKey = strings.TrimSpace(*req.PublicKey)
	}

	now := time.Now()
	token, err := generateCardToken(h.config.CardDataTokenSecret, CardTokenClaims{
		TokenType: tokenType,
		CardID:    req.CardID,
		PublicKey: publicKey,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(cardDataTokenTTL).Unix(),
	})
	if err != nil {
		logger.Error("failed to sign card token", zap.String("token_type", tokenType), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	method := http.MethodGet
	if tokenType == cardTokenTypePinChange {
		method = http.MethodPost
	}

	h.sendJSON(w, http.StatusOK, models.CardTokenResponse{
		Token: token,
		Links: []models.CardTokenLink{{
			Href:   h.config.PublicBaseURL + dataPath,
			Rel:    "data",
			Method: method,
		}},
	})
}

// sendPlaceholderCardToken serves the pre-existing opaque token for token
// types with no encrypted data endpoint behind them.
func (h *Handler) sendPlaceholderCardToken(w http.ResponseWriter, tokenType, cardID string) {
	token := fmt.Sprintf("mock-%s-%s", tokenType, cardID)
	h.sendJSON(w, http.StatusOK, models.CardTokenResponse{
		Token: token,
		Links: []models.CardTokenLink{{
			Href:   fmt.Sprintf("/cards/v1/proxy/clientDevice/%s?token=%s", tokenType, token),
			Rel:    "data",
			Method: http.MethodGet,
		}},
	})
}

// bearerCardToken pulls and validates the card token from the Authorization
// header. These endpoints are reached directly by a browser with no HMAC
// headers, so the token is the whole of the authentication.
func (h *Handler) bearerCardToken(r *http.Request, wantType string) (*CardTokenClaims, error) {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return nil, errors.New("missing bearer token")
	}

	claims, err := parseCardToken(h.config.CardDataTokenSecret, strings.TrimPrefix(authz, "Bearer "))
	if err != nil {
		return nil, err
	}
	if claims.TokenType != wantType {
		return nil, fmt.Errorf("token is for %q, not %q", claims.TokenType, wantType)
	}
	return claims, nil
}

// GetCardData returns the card's sensitive details, encrypted with the public
// key supplied when the token was minted. Excluded from HMAC authentication
// because the browser calls it directly.
// GET /cards/v1/token/card-data/data
func (h *Handler) GetCardData(w http.ResponseWriter, r *http.Request) {
	logger.Info("get card data called")

	claims, err := h.bearerCardToken(r, cardTokenTypeCardData)
	if err != nil {
		logger.Warn("rejected card data request", zap.Error(err))
		h.sendError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	// Field casing matches what consumers deserialize into.
	payload := map[string]string{
		"Pan":        generateUnmaskedPAN(claims.CardID),
		"ExpiryDate": cardDataExpiryDate(h, claims.CardID),
		"Cvc2":       randomDigits(3),
	}

	h.sendEncryptedCardPayload(w, claims.PublicKey, payload)
}

// cardDataExpiryDate reports the stored card's expiry where we have one, so the
// sensitive view agrees with the card object the consumer already holds.
func cardDataExpiryDate(h *Handler, cardID string) string {
	if card, err := h.store.GetCard(cardID); err == nil && card.ExpiryDate != "" {
		// Stored as YYYY-MM-DD; the sensitive view uses MM/YYYY.
		if t, err := time.Parse("2006-01-02", card.ExpiryDate); err == nil {
			return t.Format("01/2006")
		}
	}
	return "12/2030"
}

// GetCardPin returns the card's PIN, encrypted with the public key supplied
// when the token was minted.
// GET /cards/v1/token/pin/data
func (h *Handler) GetCardPin(w http.ResponseWriter, r *http.Request) {
	logger.Info("get card pin called")

	claims, err := h.bearerCardToken(r, cardTokenTypePin)
	if err != nil {
		logger.Warn("rejected card pin request", zap.Error(err))
		h.sendError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	pin, err := h.store.GetCardPIN(claims.CardID)
	if err != nil || pin == "" {
		// A card whose PIN was never set still has one from the consumer's
		// point of view, so derive a stable value rather than erroring.
		pin = generateCardPIN(claims.CardID)
	}

	h.sendEncryptedCardPayload(w, claims.PublicKey, pin)
}

// SetCardPin stores a new PIN supplied as a ciphertext encrypted against this
// service's public key, so the PIN itself never travels in clear text.
// POST /cards/v1/token/pin/data
func (h *Handler) SetCardPin(w http.ResponseWriter, r *http.Request) {
	logger.Info("set card pin called")

	claims, err := h.bearerCardToken(r, cardTokenTypePinChange)
	if err != nil {
		logger.Warn("rejected card pin change", zap.Error(err))
		h.sendError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	var req struct {
		Cypher string `json:"cypher"`
	}
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Cypher == "" {
		h.sendError(w, http.StatusBadRequest, "cypher is required")
		return
	}

	plaintext, err := decryptWithServiceKey(req.Cypher)
	if err != nil {
		logger.Warn("could not decrypt supplied pin", zap.Error(err))
		h.sendError(w, http.StatusBadRequest,
			"could not decrypt cypher; it must be encrypted with this service's public key from GET /cards/v1/token/pin/public-key")
		return
	}

	pin := strings.TrimSpace(string(plaintext))
	if !isPlausiblePIN(pin) {
		h.sendError(w, http.StatusBadRequest, "decrypted pin must be 4 to 12 digits")
		return
	}

	if err := h.store.SetCardPIN(claims.CardID, pin); err != nil {
		logger.Error("failed to store card pin", zap.String("card_id", claims.CardID), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "failed to store pin")
		return
	}

	logger.Info("card pin updated", zap.String("card_id", claims.CardID))
	h.sendJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// GetCardPinPublicKey publishes the key a caller encrypts a new PIN to. Without
// it a caller has no way to produce a cypher this service can open.
// GET /cards/v1/token/pin/public-key
func (h *Handler) GetCardPinPublicKey(w http.ResponseWriter, r *http.Request) {
	publicKey, err := servicePublicKeyBase64()
	if err != nil {
		logger.Error("failed to expose service public key", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "public key unavailable")
		return
	}
	h.sendJSON(w, http.StatusOK, map[string]string{
		"publicKey": publicKey,
		"format":    "base64-spki",
		"algorithm": "RSA-PKCS1v15",
	})
}

// sendEncryptedCardPayload encrypts payload to the caller's public key and
// returns it under "cypher", which is the field consumers decrypt.
func (h *Handler) sendEncryptedCardPayload(w http.ResponseWriter, publicKey string, payload interface{}) {
	if publicKey == "" {
		h.sendError(w, http.StatusBadRequest, "token carries no publicKey")
		return
	}

	var plaintext []byte
	switch v := payload.(type) {
	case string:
		// A bare value is sent as-is: consumers read the decrypted PIN
		// directly rather than parsing it out of an object.
		plaintext = []byte(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, "failed to encode card data")
			return
		}
		plaintext = encoded
	}

	cypher, err := encryptWithBase64SPKI(publicKey, plaintext)
	if err != nil {
		logger.Warn("card payload encryption failed", zap.Error(err))
		h.sendError(w, http.StatusBadRequest, "failed to encrypt with the supplied publicKey: "+err.Error())
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]string{"cypher": cypher})
}

// isPlausiblePIN keeps obvious rubbish out of storage without pretending to
// enforce a real issuer's PIN policy.
func isPlausiblePIN(pin string) bool {
	if len(pin) < 4 || len(pin) > 12 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CreateCardTransaction creates a card transaction
func (h *Handler) CreateCardTransaction(w http.ResponseWriter, r *http.Request) {
	logger.Info("create card transaction called")

	var req models.CreateCardTransactionArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// cardGuid is the interledger-app spelling of the same field.
	cardUUID := req.CardID
	if cardUUID == "" {
		cardUUID = req.CardGUID
	}

	txID := fmt.Sprintf("tx-%s", utils.GenerateUUID()[:8])
	now := time.Now().UTC().Format(time.RFC3339)

	// Consumers key off the integer id and cardId, so both must be populated.
	seqID, err := h.store.NextCardTransactionSeqID()
	if err != nil {
		logger.Error("failed to allocate card transaction sequence id", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "failed to allocate transaction id")
		return
	}
	numericCard := numericCardID(cardUUID)

	tx := &models.CardTransaction{
		ID:                    &seqID,
		CardID:                &numericCard,
		TransactionID:         txID,
		GHResponseCode:        "00",
		GHResponseDescription: "Approved",
		TransactionAmount:     &req.Amount,
		TransactionCurrency:   &req.Currency,
		BillingAmount:         &req.Amount,
		BillingCurrency:       &req.Currency,
		Type:                  req.Type,
		CardScheme:            2,
		TerminalID:            "TERM001",
		CreatedAt:             now,
		TxStatus:              strPtr("COMPLETED"),
		Operation:             0,
		MerchantName:          req.MerchantName,
		MerchantCity:          req.MerchantCity,
		MerchantCountry:       req.MerchantCountry,
		TransactionDateTime:   &now,
		ProcessDateTime:       &now,
	}

	if err := h.store.CreateCardTransaction(tx); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create transaction")
		return
	}

	// Index by card
	if cardUUID != "" {
		_ = h.store.AddCardTransactionIndex(cardUUID, txID)
	}

	h.sendJSON(w, http.StatusCreated, tx)
}

// GetCardTransaction retrieves a single card transaction by ID
func (h *Handler) GetCardTransaction(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	logger.Info("get card transaction called", zap.String("tx_id", txID))

	// A simulated transaction is stored verbatim so that fields this service
	// does not model still reach the consumer. Prefer it when present.
	if raw, err := h.store.GetRawCardTransaction(txID); err == nil && len(raw) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(raw); err != nil {
			logger.Warn("failed to write raw card transaction", zap.Error(err))
		}
		return
	}

	tx, err := h.store.GetCardTransaction(txID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "transaction not found")
		return
	}

	h.sendJSON(w, http.StatusOK, tx)
}

// ListCardTransactions retrieves card transactions (stub)
func (h *Handler) ListCardTransactions(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	pageNumber, pageSize := parsePagination(r)
	logger.Info("list card transactions called",
		zap.String("card_id", cardID),
		zap.Int("page_number", pageNumber),
		zap.Int("page_size", pageSize),
	)

	txIDs, err := h.store.GetCardTransactionIDs(cardID)
	if err != nil {
		txIDs = nil
	}

	// Collect the payloads newest-first, preferring the verbatim JSON of a
	// simulated transaction so unmodelled fields survive the round trip.
	all := make([]json.RawMessage, 0, len(txIDs))
	for _, txID := range txIDs {
		if raw, err := h.store.GetRawCardTransaction(txID); err == nil && len(raw) > 0 {
			all = append(all, raw)
			continue
		}
		tx, err := h.store.GetCardTransaction(txID)
		if err != nil {
			continue
		}
		encoded, err := json.Marshal(tx)
		if err != nil {
			logger.Warn("failed to encode card transaction", zap.String("tx_id", txID), zap.Error(err))
			continue
		}
		all = append(all, encoded)
	}

	totalRecords := len(all)
	page := paginate(all, pageNumber, pageSize)

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"data": page,
		"pagination": map[string]interface{}{
			"pageNumber":   pageNumber,
			"pageSize":     pageSize,
			"totalPages":   totalPages(totalRecords, pageSize),
			"totalRecords": totalRecords,
		},
	})
}

// parsePagination reads the pageNumber and pageSize query parameters. Callers
// send them, so ignoring them would silently return the wrong window once a
// card has more transactions than one page.
func parsePagination(r *http.Request) (pageNumber, pageSize int) {
	pageNumber, pageSize = 1, defaultCardTxPageSize

	if raw := r.URL.Query().Get("pageNumber"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			pageNumber = v
		}
	}
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			pageSize = v
			if pageSize > maxCardTxPageSize {
				pageSize = maxCardTxPageSize
			}
		}
	}
	return pageNumber, pageSize
}

// paginate returns the requested window, or an empty page when the caller asks
// for one past the end.
func paginate(items []json.RawMessage, pageNumber, pageSize int) []json.RawMessage {
	start := (pageNumber - 1) * pageSize
	if start >= len(items) {
		return []json.RawMessage{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func totalPages(totalRecords, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	// Always report at least one page so a card with no transactions still
	// looks like a valid single empty page rather than an absent result set.
	pages := (totalRecords + pageSize - 1) / pageSize
	if pages == 0 {
		return 1
	}
	return pages
}

// CreateThreeDSChallenge creates a 3DS challenge for testing
func (h *Handler) CreateThreeDSChallenge(w http.ResponseWriter, r *http.Request) {
	logger.Info("create 3ds challenge called")

	var req struct {
		CardID           string `json:"cardId"`
		UserID           string `json:"userId"`
		MerchantName     string `json:"merchantName"`
		PurchaseAmount   string `json:"purchaseAmount"`
		PurchaseCurrency string `json:"purchaseCurrency"`
	}
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	txID := fmt.Sprintf("3ds-%s", utils.GenerateUUID()[:8])

	challenge := &models.ThreeDSChallenge{
		TransactionID:    txID,
		CardID:           req.CardID,
		UserID:           req.UserID,
		MerchantName:     req.MerchantName,
		PurchaseAmount:   req.PurchaseAmount,
		PurchaseCurrency: req.PurchaseCurrency,
		PurchaseDate:     time.Now().UTC().Format(time.RFC3339),
		Timeout:          time.Now().Add(5 * time.Minute),
		Status:           "pending",
		CreatedAt:        time.Now(),
	}

	if err := h.store.CreateThreeDSChallenge(challenge); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to create 3DS challenge")
		return
	}

	h.sendJSON(w, http.StatusCreated, challenge)
}

// ConfirmThreeDS confirms or denies a 3DS challenge
func (h *Handler) ConfirmThreeDS(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txID")
	logger.Info("confirm 3ds called", zap.String("tx_id", txID))

	var req models.ThreeDSPaymentConfirmationArgs
	if err := h.decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	challenge, err := h.store.GetThreeDSChallenge(txID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "3DS challenge not found")
		return
	}

	if req.Confirmed {
		challenge.Status = "approved"
	} else {
		challenge.Status = "declined"
	}

	if err := h.store.UpdateThreeDSChallenge(challenge); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update 3DS challenge")
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"transactionId": txID,
		"confirmed":     req.Confirmed,
	})
}

// GetCardApplicationProducts retrieves available card products
func (h *Handler) GetCardApplicationProducts(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	logger.Info("get card application products called", zap.String("app_id", appID))

	products := []map[string]interface{}{
		{
			"uuid":               utils.GenerateUUID(),
			"accountProductCode": "PROD_EUR_ACCOUNT",
			"code":               "PWSR_DEBP_2404",
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

	h.sendJSON(w, http.StatusOK, products)
}

// OrderPlasticCard orders a physical card
func (h *Handler) OrderPlasticCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	logger.Info("order plastic card called", zap.String("card_id", cardID))

	h.sendJSON(w, http.StatusCreated, map[string]interface{}{
		"orderId":   utils.GenerateUUID(),
		"cardId":    cardID,
		"status":    "PENDING",
		"type":      "PLASTIC",
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}
