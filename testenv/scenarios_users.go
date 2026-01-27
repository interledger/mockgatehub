package main

import (
    "fmt"
    "time"
)

func scenarioUserKYCAcceptance() scenario {
    return scenario{
        name: "User KYC acceptance",
        run: func(h *harness) (string, error) {
            email := fmt.Sprintf("e2e+%d@example.com", time.Now().UnixNano())
            userID, err := h.createManagedUser(email)
            if err != nil {
                return "", err
            }
            if err := h.startKYC(userID); err != nil {
                return "", err
            }
            if err := h.submitKYC(userID); err != nil {
                return "", err
            }
            user, err := h.getUser(userID)
            if err != nil {
                return "", err
            }
            if user.KYCState != "accepted" {
                return "", fmt.Errorf("expected kyc_state accepted, got %s", user.KYCState)
            }
            return fmt.Sprintf("user %s accepted", userID), nil
        },
    }
}
