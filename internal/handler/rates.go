package handler

import (
	"net/http"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"
)

// GetCurrentRates returns exchange rates for all supported currencies.
// SandboxRates stores each currency's value in USD (e.g. EUR=1.08 means 1 EUR = 1.08 USD).
// The counter param selects the denomination: counter=EUR means "how many EUR per 1 unit".
// Conversion: rate_in_counter = rate_in_USD / counter_rate_in_USD.
func (h *Handler) GetCurrentRates(w http.ResponseWriter, r *http.Request) {
	logger.Info("getting current exchange rates")

	// Get counter currency from query param (default USD)
	counter := r.URL.Query().Get("counter")
	if counter == "" {
		counter = "USD"
	}

	counterRateInUSD, ok := consts.SandboxRates[counter]
	if !ok {
		h.sendError(w, http.StatusBadRequest, "unsupported counter currency: "+counter)
		return
	}

	// Build response in GateHub format: flat object with counter and currency rates
	response := map[string]interface{}{
		"counter": counter,
	}

	// Convert each currency rate from USD-denominated to counter-denominated
	for currency, rateInUSD := range consts.SandboxRates {
		response[currency] = map[string]interface{}{
			"type":   "ExchangeRate",
			"rate":   rateInUSD / counterRateInUSD,
			"amount": "1",
			"change": "0",
		}
	}

	h.sendJSON(w, http.StatusOK, response)
}

// GetVaults returns liquidity vault UUIDs for all currencies
func (h *Handler) GetVaults(w http.ResponseWriter, r *http.Request) {
	logger.Info("getting liquidity vaults")

	var vaults []models.VaultItem
	for currency, uuid := range consts.SandboxVaultIDs {
		vaults = append(vaults, models.VaultItem{
			Currency: currency,
			UUID:     uuid,
		})
	}

	response := models.GetVaultsResponse{
		Vaults: vaults,
	}

	h.sendJSON(w, http.StatusOK, response)
}
