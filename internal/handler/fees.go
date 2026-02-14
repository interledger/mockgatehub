package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"sync"

	"mockgatehub/internal/logger"

	"go.uber.org/zap"
)

// FeeConfig holds configurable fee percentages (thread-safe).
type FeeConfig struct {
	mu                   sync.RWMutex
	depositFeePercent    float64
	withdrawalFeePercent float64
}

// NewFeeConfig creates a FeeConfig with 0% defaults.
func NewFeeConfig() *FeeConfig {
	return &FeeConfig{}
}

// GetDepositFeePercent returns the current deposit fee percentage.
func (fc *FeeConfig) GetDepositFeePercent() float64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.depositFeePercent
}

// GetWithdrawalFeePercent returns the current withdrawal fee percentage.
func (fc *FeeConfig) GetWithdrawalFeePercent() float64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.withdrawalFeePercent
}

// SetDepositFeePercent sets the deposit fee percentage.
func (fc *FeeConfig) SetDepositFeePercent(pct float64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.depositFeePercent = pct
}

// SetWithdrawalFeePercent sets the withdrawal fee percentage.
func (fc *FeeConfig) SetWithdrawalFeePercent(pct float64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.withdrawalFeePercent = pct
}

// CalculateFee computes fee = amount * (percent/100), rounded to 2 decimals.
func CalculateFee(amount, percent float64) float64 {
	raw := amount * percent / 100.0
	return math.Round(raw*100) / 100
}

// ── Admin endpoints ──────────────────────────────────────────────────

// feeResponse is the JSON shape for GET/PUT /admin/fees.
type feeResponse struct {
	DepositFeePercentage    float64 `json:"deposit_fee_percentage"`
	WithdrawalFeePercentage float64 `json:"withdrawal_fee_percentage"`
}

// GetFees returns the current fee configuration.
func (h *Handler) GetFees(w http.ResponseWriter, r *http.Request) {
	resp := feeResponse{
		DepositFeePercentage:    h.feeConfig.GetDepositFeePercent(),
		WithdrawalFeePercentage: h.feeConfig.GetWithdrawalFeePercent(),
	}
	h.sendJSON(w, http.StatusOK, resp)
}

// SetFees updates the fee configuration.
func (h *Handler) SetFees(w http.ResponseWriter, r *http.Request) {
	var req feeResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate
	if req.DepositFeePercentage < 0 || req.DepositFeePercentage > 100 {
		h.sendError(w, http.StatusBadRequest, "deposit_fee_percentage must be between 0 and 100")
		return
	}
	if req.WithdrawalFeePercentage < 0 || req.WithdrawalFeePercentage > 100 {
		h.sendError(w, http.StatusBadRequest, "withdrawal_fee_percentage must be between 0 and 100")
		return
	}

	h.feeConfig.SetDepositFeePercent(req.DepositFeePercentage)
	h.feeConfig.SetWithdrawalFeePercent(req.WithdrawalFeePercentage)

	logger.Info("fee configuration updated",
		zap.Float64("deposit_fee_pct", req.DepositFeePercentage),
		zap.Float64("withdrawal_fee_pct", req.WithdrawalFeePercentage),
	)

	resp := feeResponse{
		DepositFeePercentage:    h.feeConfig.GetDepositFeePercent(),
		WithdrawalFeePercentage: h.feeConfig.GetWithdrawalFeePercent(),
	}
	h.sendJSON(w, http.StatusOK, resp)
}
