package main

import (
    "fmt"
    "time"
)

func scenarioExternalDeposit() scenario {
    return scenario{
        name: "External deposit transaction",
        run: func(h *harness) (string, error) {
            email := fmt.Sprintf("extdep+%d@example.com", time.Now().UnixNano())
            userID, err := h.bootstrapAcceptedUser(email)
            if err != nil {
                return "", err
            }
            wallet, err := h.createWallet(userID, "External Deposit Wallet")
            if err != nil {
                return "", err
            }
            tx, err := h.createTransaction(transactionRequest{
                UserID:           userID,
                Amount:           123.45,
                Currency:         "EUR",
                VaultUUID:        "a09a0a2c-1a3a-44c5-a1b9-603a6eea9341",
                ReceivingAddress: wallet.Address,
                Type:             1,
                DepositType:      "external",
            })
            if err != nil {
                return "", err
            }
            if tx.Status != 1 {
                return "", fmt.Errorf("expected status 1, got %d", tx.Status)
            }
            return fmt.Sprintf("external deposit tx %s", tx.ID), nil
        },
    }
}

func scenarioHostedTransfer() scenario {
    return scenario{
        name: "Hosted transfer transaction",
        run: func(h *harness) (string, error) {
            email := fmt.Sprintf("hosted+%d@example.com", time.Now().UnixNano())
            userID, err := h.bootstrapAcceptedUser(email)
            if err != nil {
                return "", err
            }
            tx, err := h.createTransaction(transactionRequest{
                UserID:      userID,
                Amount:      150.00,
                Currency:    "USD",
                Type:        2,
                DepositType: "hosted",
            })
            if err != nil {
                return "", err
            }
            if tx.Status != 1 {
                return "", fmt.Errorf("expected status 1, got %d", tx.Status)
            }
            if tx.DepositType != "hosted" {
                return "", fmt.Errorf("expected deposit_type hosted, got %s", tx.DepositType)
            }
            return fmt.Sprintf("hosted transfer tx %s", tx.ID), nil
        },
    }
}

func scenarioIframeDeposit() scenario {
    return scenario{
        name: "Iframe deposit completion",
        run: func(h *harness) (string, error) {
            email := fmt.Sprintf("iframe+%d@example.com", time.Now().UnixNano())
            userID, err := h.bootstrapAcceptedUser(email)
            if err != nil {
                return "", err
            }
            iframeToken, err := h.getIframeToken(userID)
            if err != nil {
                return "", err
            }
            wallet, err := h.createWallet(userID, "Iframe Wallet")
            if err != nil {
                return "", err
            }
            if err := h.completeDeposit(iframeToken, 75.50, "EUR"); err != nil {
                return "", err
            }
            balances, err := h.getBalances(wallet.Address)
            if err != nil {
                return "", err
            }
            if len(balances) == 0 {
                return "", fmt.Errorf("no balances after iframe deposit")
            }
            return fmt.Sprintf("iframe deposit completed for user %s", userID), nil
        },
    }
}
