package main

import "fmt"

func scenarioRatesAndVaults() scenario {
    return scenario{
        name: "Rates and vaults",
        run: func(h *harness) (string, error) {
            rates := map[string]interface{}{}
            if err := h.getJSON("/rates/v1/rates/current", &rates, nil); err != nil {
                return "", err
            }
            if _, ok := rates["counter"].(string); !ok {
                return "", fmt.Errorf("counter currency missing")
            }
            if len(rates) < 2 {
                return "", fmt.Errorf("no currency rates returned")
            }

            vaults := map[string]interface{}{}
            if err := h.getJSON("/rates/v1/liquidity_provider/vaults", &vaults, nil); err != nil {
                return "", err
            }
            arr, ok := vaults["vaults"].([]interface{})
            if !ok || len(arr) == 0 {
                return "", fmt.Errorf("vaults array empty")
            }
            return fmt.Sprintf("rates and %d vaults retrieved", len(arr)), nil
        },
    }
}
