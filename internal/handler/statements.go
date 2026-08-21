package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/pdf"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// GetAccountConfirmation returns a document confirming an account exists.
// GET /statement/v1/statements/account-confirmation/{walletAddress}
func (h *Handler) GetAccountConfirmation(w http.ResponseWriter, r *http.Request) {
	walletAddress := chi.URLParam(r, "walletAddress")
	if walletAddress == "" {
		h.sendError(w, http.StatusBadRequest, "wallet address is required")
		return
	}

	logger.Info("account confirmation requested", zap.String("wallet_address", walletAddress))

	lines := []string{
		"Wallet address: " + walletAddress,
		"",
	}

	// Name the holder where we know them, so a caller can tell one
	// confirmation from another.
	if wallet, err := h.store.GetWallet(walletAddress); err == nil && wallet != nil {
		lines = append(lines,
			"Account holder: "+h.accountHolderName(wallet.UserID),
			"Wallet name: "+wallet.Name,
		)
	} else {
		logger.Debug("account confirmation for an unknown wallet", zap.String("wallet_address", walletAddress))
		lines = append(lines, "Account holder: not on record")
	}

	lines = append(lines, "", "Issued: "+time.Now().UTC().Format(time.RFC1123))

	h.sendStatementPDF(w, "account-confirmation.pdf", pdf.Document{
		Title: "Account Confirmation",
		Lines: lines,
	})
}

// GetAccountStatement returns a statement of activity for one month.
// GET /statement/v1/statements/account-statement/{walletAddress}/{year}/{month}
func (h *Handler) GetAccountStatement(w http.ResponseWriter, r *http.Request) {
	walletAddress := chi.URLParam(r, "walletAddress")
	if walletAddress == "" {
		h.sendError(w, http.StatusBadRequest, "wallet address is required")
		return
	}

	year, month, err := parseStatementPeriod(chi.URLParam(r, "year"), chi.URLParam(r, "month"))
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	logger.Info("account statement requested",
		zap.String("wallet_address", walletAddress),
		zap.Int("year", year),
		zap.Int("month", month),
		zap.String("networks", r.URL.Query().Get("networks")),
		zap.String("gateways", r.URL.Query().Get("gateways")),
	)

	period := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lines := []string{
		"Wallet address: " + walletAddress,
		"Period: " + period.Format("January 2006"),
		"",
	}

	if wallet, err := h.store.GetWallet(walletAddress); err == nil && wallet != nil {
		lines = append(lines, "Account holder: "+h.accountHolderName(wallet.UserID), "")
		lines = append(lines, h.statementActivityLines(wallet.UserID, year, month)...)
	} else {
		lines = append(lines, "Account holder: not on record")
	}

	lines = append(lines, "", "Issued: "+time.Now().UTC().Format(time.RFC1123))

	h.sendStatementPDF(w, "account-statement.pdf", pdf.Document{
		Title: "Account Statement",
		Lines: lines,
	})
}

// GetTransferConfirmation returns a document confirming one transfer.
// GET /statement/v1/statements/transfer-confirmation/{transactionUUID}
func (h *Handler) GetTransferConfirmation(w http.ResponseWriter, r *http.Request) {
	transactionUUID := chi.URLParam(r, "transactionUUID")
	if transactionUUID == "" {
		h.sendError(w, http.StatusBadRequest, "transaction UUID is required")
		return
	}

	tx, err := h.store.GetTransaction(transactionUUID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "transaction not found")
		return
	}

	// Only a movement of money into or out of the account has a transfer to
	// confirm; a hosted transfer between internal wallets does not.
	switch tx.Type {
	case consts.TransactionTypeDeposit, consts.TransactionTypeWithdrawal:
	default:
		h.sendError(w, http.StatusBadRequest,
			fmt.Sprintf("transaction %s is neither a deposit nor a withdrawal", transactionUUID))
		return
	}

	logger.Info("transfer confirmation requested",
		zap.String("transaction_uuid", transactionUUID),
		zap.String("deposit_type", tx.DepositType),
	)

	h.sendStatementPDF(w, "transfer-confirmation.pdf", pdf.Document{
		Title: "Transfer Confirmation",
		Lines: []string{
			"Transaction: " + tx.ID,
			"Type: " + tx.DepositType,
			"Status: " + transactionStatusName(tx.Status),
			"",
			"Amount: " + tx.Amount + " " + tx.Currency,
			"Fee: " + tx.Fee + " " + tx.Currency,
			"Total: " + tx.TotalAmount + " " + tx.Currency,
			"",
			"Sending address: " + orNotRecorded(tx.SendingAddress),
			"Receiving address: " + orNotRecorded(tx.ReceivingAddress),
			"",
			"Created: " + tx.CreatedAt.UTC().Format(time.RFC1123),
			"Issued: " + time.Now().UTC().Format(time.RFC1123),
		},
	})
}

// statementActivityLines summarises the account's transactions for the period.
func (h *Handler) statementActivityLines(userID string, year, month int) []string {
	transactions, err := h.store.ListTransactionsByUser(userID)
	if err != nil {
		logger.Warn("could not list transactions for statement", zap.String("user_id", userID), zap.Error(err))
		return []string{"Activity unavailable"}
	}

	lines := []string{"Transactions"}
	count := 0
	for _, tx := range transactions {
		created := tx.CreatedAt.UTC()
		if created.Year() != year || int(created.Month()) != month {
			continue
		}
		count++
		lines = append(lines, fmt.Sprintf("  %s  %-10s  %10s %s  %s",
			created.Format("2006-01-02"), tx.DepositType, tx.Amount, tx.Currency, tx.ID))
	}

	if count == 0 {
		return []string{"Transactions", "  No activity in this period"}
	}
	return append(lines, "", fmt.Sprintf("%d transaction(s) in period", count))
}

func (h *Handler) accountHolderName(userID string) string {
	user, err := h.store.GetUser(userID)
	if err != nil {
		return "not on record"
	}
	name := joinNonEmpty(" ", user.FirstName, user.LastName)
	if name == "" {
		return user.Email
	}
	return name
}

// parseStatementPeriod validates the year and month path parameters. A
// nonsensical period must be refused rather than silently rendered.
func parseStatementPeriod(yearParam, monthParam string) (year, month int, err error) {
	if yearParam == "" {
		return 0, 0, fmt.Errorf("year is required")
	}
	if monthParam == "" {
		return 0, 0, fmt.Errorf("month is required")
	}

	year, err = strconv.Atoi(yearParam)
	if err != nil || year < 1970 || year > 9999 {
		return 0, 0, fmt.Errorf("invalid year %q: expected a four-digit year", yearParam)
	}

	month, err = strconv.Atoi(monthParam)
	if err != nil || month < 1 || month > 12 {
		return 0, 0, fmt.Errorf("invalid month %q: expected 1 to 12", monthParam)
	}

	return year, month, nil
}

func transactionStatusName(status int) string {
	switch status {
	case consts.TransactionStatusPending:
		return "pending"
	case consts.TransactionStatusCompleted:
		return "completed"
	case consts.TransactionStatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown (%d)", status)
	}
}

func orNotRecorded(value string) string {
	if value == "" {
		return "not recorded"
	}
	return value
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	out := kept[0]
	for _, p := range kept[1:] {
		out += sep + p
	}
	return out
}

// sendStatementPDF renders and returns a statement as a downloadable PDF.
func (h *Handler) sendStatementPDF(w http.ResponseWriter, filename string, doc pdf.Document) {
	body := pdf.Render(doc)

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(body); err != nil {
		logger.Warn("failed to write statement pdf", zap.String("filename", filename), zap.Error(err))
	}
}
