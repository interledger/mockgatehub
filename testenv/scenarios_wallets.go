package main

import (
    "fmt"
    "time"
)

func scenarioWalletsAndBalances() scenario {
    return scenario{
        name: "Wallet creation and balances",
        run: func(h *harness) (string, error) {
            email := fmt.Sprintf("wallet+%d@example.com", time.Now().UnixNano())
            userID, err := h.bootstrapAcceptedUser(email)
            if err != nil {
                return "", err
            }
            wallet, err := h.createWallet(userID, "My Wallet")
            if err != nil {
                return "", err
            }
            balances, err := h.getBalances(wallet.Address)
            if err != nil {
                return "", err
            }
            if len(balances) < 11 {
                return "", fmt.Errorf("expected 11 balances, got %d", len(balances))
            }
            return fmt.Sprintf("wallet %s with %d balances", wallet.Address, len(balances)), nil
        },
    }
}
