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

// Phase 6: Card Products & Plastic
type cardProductsResponse struct {
	Data       []cardProduct `json:"data"`
	Pagination pagination    `json:"pagination"`
}

type cardProduct struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Currency    string `json:"currency"`
	Active      bool   `json:"active"`
}

type pagination struct {
	PageNumber int `json:"pageNumber"`
	PageSize   int `json:"pageSize"`
	TotalPages int `json:"totalPages"`
}

type plasticCardResponse struct {
	Message         string                  `json:"message"`
	OrderID         string                  `json:"orderId"`
	CardID          string                  `json:"cardId"`
	Status          string                  `json:"status"`
	Type            string                  `json:"type"`
	EstimatedDate   string                  `json:"estimatedDate"`
	DeliveryAddress *plasticDeliveryAddress `json:"deliveryAddress"`
}

type plasticDeliveryAddress struct {
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	AddressLine string `json:"addressLine1"`
	City        string `json:"city"`
	ZipCode     string `json:"zipCode"`
	Country     string `json:"country"`
}
