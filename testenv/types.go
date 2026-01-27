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
