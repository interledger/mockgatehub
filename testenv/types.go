package main

import "net/http"

const (
	mockGatehubURL = "http://localhost:25151"
	maxWaitSeconds = 30
	testAppID      = "local-test-app-id"
	testAppSecret  = "local-test-app-secret"
)

type harness struct {
	client *http.Client
}

type scenario struct {
	name string
	run  func(h *harness) (string, error)
}

type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	KYCState  string `json:"kyc_state"`
	RiskLevel string `json:"risk_level"`
}

type walletResponse struct {
	Address string `json:"address"`
	UserID  string `json:"user_id"`
}

type transactionRequest struct {
	UserID           string  `json:"user_id"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
	VaultUUID        string  `json:"vault_uuid,omitempty"`
	ReceivingAddress string  `json:"receiving_address,omitempty"`
	Type             int     `json:"type"`
	DepositType      string  `json:"deposit_type,omitempty"`
}

type transactionResponse struct {
	ID          string `json:"id"`
	UUID        string `json:"uuid"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Status      int    `json:"status"`
	DepositType string `json:"deposit_type"`
}

type balanceEntry struct {
	Currency  string                 `json:"currency"`
	Vault     map[string]interface{} `json:"vault"`
	Available string                 `json:"available"`
}

// Card API fixtures
type cardCustomerResponse struct {
	WalletAddress string       `json:"walletAddress"`
	Customer      cardCustomer `json:"customers"`
}

type cardCustomer struct {
	ID        *string       `json:"id"`
	Accounts  []cardAccount `json:"accounts"`
	Addresses []cardAddress `json:"addresses"`
}

type cardAccount struct {
	ID      *string    `json:"id"`
	Cards   []cardItem `json:"cards"`
	Status  string     `json:"status"`
	Product string     `json:"productCode"`
	Number  string     `json:"accountNumber"`
	Created string     `json:"createdAt"`
}

type cardItem struct {
	ID               string `json:"id"`
	AccountID        string `json:"accountId"`
	CustomerID       string `json:"customerId"`
	Status           string `json:"status"`
	MaskedPan        string `json:"maskedPan"`
	ExpiryDate       string `json:"expiryDate"`
	RelationType     string `json:"relationType"`
	NameOnCard       string `json:"nameOnCard"`
	PanToken         string `json:"panToken"`
	ProductCode      string `json:"productCode"`
	OrderPlasticSync bool   `json:"OrderPlasticOnSync"`
}

type cardAddress struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Line1       string  `json:"line1"`
	City        string  `json:"city"`
	CountryCode string  `json:"countryCode"`
	ZipCode     string  `json:"zipCode"`
	Status      string  `json:"status"`
	Line2       *string `json:"line2"`
	Line3       *string `json:"line3"`
}

type cardLimit struct {
	Type       string  `json:"type"`
	Limit      float64 `json:"limit"`
	Currency   string  `json:"currency"`
	IsDisabled bool    `json:"isDisabled"`
}

type cardLimitsResponse struct {
	Limits []cardLimit `json:"limits"`
}

type cardTokenResponse struct {
	Token string `json:"token"`
}

type cardTransactionResponse struct {
	TransactionID string  `json:"transactionId"`
	Amount        *string `json:"transactionAmount"`
	Currency      *string `json:"transactionCurrency"`
	Type          int     `json:"type"`
	CreatedAt     string  `json:"createdAt"`
}

type pending3DSResponse struct {
	PendingConfirmations []pending3DSItem `json:"pendingConfirmations"`
}

type pending3DSItem struct {
	TransactionID    string `json:"transactionId"`
	MerchantName     string `json:"merchantName"`
	PurchaseAmount   string `json:"purchaseAmount"`
	PurchaseCurrency string `json:"purchaseCurrency"`
	PurchaseDate     string `json:"purchaseDate"`
	Timeout          string `json:"timeout"`
}
