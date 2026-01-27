package main

import (
	"fmt"
	"time"
)

func scenarioCards() scenario {
	return scenario{
		name: "Cards lifecycle and 3DS",
		run: func(h *harness) (string, error) {
			email := fmt.Sprintf("cards+%d@example.com", time.Now().UnixNano())
			userID, err := h.bootstrapAcceptedUser(email)
			if err != nil {
				return "", err
			}

			wallet, err := h.createWallet(userID, "Cards Wallet")
			if err != nil {
				return "", err
			}

			customer, err := h.createCustomerAndCard(userID, wallet.Address, "Test User")
			if err != nil {
				return "", err
			}

			if len(customer.Customer.Accounts) == 0 || len(customer.Customer.Accounts[0].Cards) == 0 {
				return "", fmt.Errorf("customer missing account/card")
			}

			accountID := derefString(customer.Customer.Accounts[0].ID)
			primaryCard := customer.Customer.Accounts[0].Cards[0]

			address, err := h.createDeliveryAddress(userID, derefString(customer.Customer.ID))
			if err != nil {
				return "", err
			}
			if address.ID == "" {
				return "", fmt.Errorf("delivery address missing id")
			}

			newCard, err := h.orderAdditionalCard(userID, accountID, wallet.Address)
			if err != nil {
				return "", err
			}

			cardDetails, err := h.getCard(userID, newCard.ID)
			if err != nil {
				return "", err
			}
			if cardDetails.Status == "" || cardDetails.MaskedPan == "" {
				return "", fmt.Errorf("card details incomplete")
			}

			token, err := h.getCardToken(userID, primaryCard.ID)
			if err != nil {
				return "", err
			}
			if token == "" {
				return "", fmt.Errorf("empty card token")
			}

			limits, err := h.getCardLimits(userID, primaryCard.ID)
			if err != nil {
				return "", err
			}
			if len(limits) == 0 {
				return "", fmt.Errorf("card limits missing")
			}

			// toggle first limit disabled flag to ensure update works
			limits[0].IsDisabled = !limits[0].IsDisabled
			if err := h.updateCardLimits(userID, primaryCard.ID, limits); err != nil {
				return "", err
			}

			tx, err := h.createCardTransaction(userID, primaryCard.ID)
			if err != nil {
				return "", err
			}
			if tx.TransactionID == "" {
				return "", fmt.Errorf("card transaction missing id")
			}

			txs, err := h.listCardTransactions(userID, primaryCard.ID)
			if err != nil {
				return "", err
			}
			if len(txs) == 0 {
				return "", fmt.Errorf("expected at least one transaction")
			}

			threeDSTxID, err := h.createThreeDSChallenge(userID, primaryCard.ID)
			if err != nil {
				return "", err
			}

			pending, err := h.getPendingConfirmations(userID)
			if err != nil {
				return "", err
			}
			if len(pending.PendingConfirmations) == 0 {
				return "", fmt.Errorf("expected pending 3ds confirmation")
			}

			if err := h.confirmThreeDS(userID, threeDSTxID, true); err != nil {
				return "", err
			}

			return fmt.Sprintf("card %s 3ds %s", primaryCard.ID, threeDSTxID), nil
		},
	}
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
