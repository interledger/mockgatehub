package handler

import (
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// uiTemplates holds the admin views. They are embedded so the binary is
// self-contained and the UI cannot go missing in a container image.
//
//go:embed web/ui/*.html
var uiTemplates embed.FS

// uiPages maps a view to the template file providing its "content" block.
var uiPages = map[string]string{
	"dashboard": "web/ui/dashboard.html",
	"user":      "web/ui/user.html",
	"kyc":       "web/ui/kyc_action.html",
	"cardTx":    "web/ui/card_tx_action.html",
}

// uiPageTemplates is built once at startup; a template parse failure is a
// build-time mistake in files that ship with the binary.
var uiPageTemplates = mustParseUITemplates()

func mustParseUITemplates() map[string]*template.Template {
	parsed := make(map[string]*template.Template, len(uiPages))
	for name, file := range uiPages {
		parsed[name] = template.Must(template.ParseFS(uiTemplates, "web/ui/layout.html", file))
	}
	return parsed
}

// uiView is the data every admin page renders with.
type uiView struct {
	Title        string
	Flash        string
	FlashIsError bool

	Users        []uiUserSummary
	User         *models.User
	FullName     string
	Balances     map[string]float64
	Cards        []*models.Card
	Transactions []uiTransaction
	Scenarios    []CardTxScenario
	Outcomes     []uiOption
	Events       []uiOption

	SelectedUserID string
	SelectedCardID string
}

type uiUserSummary struct {
	User     *models.User
	Balances map[string]float64
}

// uiTransaction decorates a transaction with what the view needs to decide, so
// the template holds no business rules of its own.
type uiTransaction struct {
	*models.Transaction
	StatusName string
	// CanSettle is true only for a withdrawal still awaiting an outcome.
	CanSettle bool
}

type uiOption struct {
	Value string
	Label string
}

// kycOutcomeOptions are the verification outcomes the UI can trigger.
func kycOutcomeOptions() []uiOption {
	return []uiOption{
		{consts.KYCStateAccepted, "Accepted"},
		{consts.KYCStateRejected, "Rejected"},
		{consts.KYCStateActionRequired, "Action required"},
		{consts.KYCStateResubmission, "Resubmission requested"},
		{consts.WebhookEventDocumentNoticeWarning, "Document notice: expiring soon"},
		{consts.WebhookEventDocumentNoticeExpired, "Document notice: expired"},
	}
}

// cardTxEventOptions are the webhooks a simulated card transaction can emit.
func cardTxEventOptions() []uiOption {
	return []uiOption{
		{consts.WebhookEventCardTransactionAuthorization, "Authorization (carries the transaction)"},
		{consts.WebhookEventCardTransaction, "Notification only (no transaction)"},
	}
}

// renderUI writes one admin page.
func (h *Handler) renderUI(w http.ResponseWriter, page string, view uiView) {
	tmpl, ok := uiPageTemplates[page]
	if !ok {
		logger.Error("unknown admin ui page", zap.String("page", page))
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", view); err != nil {
		// The response is already partly written by this point, so there is
		// nothing useful to send; log it so the failure is not silent.
		logger.Error("failed to render admin ui page", zap.String("page", page), zap.Error(err))
	}
}

// flashFrom reads a message passed back through a redirect.
func flashFrom(r *http.Request) (message string, isError bool) {
	return r.URL.Query().Get("flash"), r.URL.Query().Get("error") == "1"
}

// redirectWithFlash sends the browser onward carrying a message.
func (h *Handler) redirectWithFlash(w http.ResponseWriter, r *http.Request, path, message string, isError bool) {
	target := path + "?flash=" + url.QueryEscape(message)
	if isError {
		target += "&error=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// userSummaries loads every user with their balances, for the listings.
func (h *Handler) userSummaries() []uiUserSummary {
	users, err := h.store.ListUsers()
	if err != nil {
		logger.Error("failed to list users for admin ui", zap.Error(err))
		return nil
	}

	summaries := make([]uiUserSummary, 0, len(users))
	for _, user := range users {
		balances, err := h.store.GetAllBalances(user.ID)
		if err != nil {
			logger.Warn("failed to load balances for admin ui", zap.String("user_id", user.ID), zap.Error(err))
		}
		summaries = append(summaries, uiUserSummary{User: user, Balances: balances})
	}
	return summaries
}

// UIDashboard lists users and their balances.
// GET /ui
func (h *Handler) UIDashboard(w http.ResponseWriter, r *http.Request) {
	flash, isError := flashFrom(r)
	h.renderUI(w, "dashboard", uiView{
		Title:        "Users",
		Flash:        flash,
		FlashIsError: isError,
		Users:        h.userSummaries(),
	})
}

// UIUserDetail shows one user's balances, cards and transactions.
// GET /ui/users/{userID}
func (h *Handler) UIUserDetail(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	user, err := h.store.GetUser(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	balances, err := h.store.GetAllBalances(userID)
	if err != nil {
		logger.Warn("failed to load balances", zap.String("user_id", userID), zap.Error(err))
	}

	flash, isError := flashFrom(r)
	h.renderUI(w, "user", uiView{
		Title:        user.Email,
		Flash:        flash,
		FlashIsError: isError,
		User:         user,
		FullName:     joinNonEmpty(" ", user.FirstName, user.LastName),
		Balances:     balances,
		Cards:        h.cardsForUser(userID),
		Transactions: h.transactionsForUser(userID),
	})
}

// cardsForUser collects the user's cards across their card customers.
func (h *Handler) cardsForUser(userID string) []*models.Card {
	customer, err := h.store.GetCustomerBySourceID(userID)
	if err != nil || customer == nil || customer.ID == nil {
		return nil
	}

	cards, err := h.store.GetCardsByCustomer(*customer.ID)
	if err != nil {
		logger.Warn("failed to load cards for admin ui", zap.String("user_id", userID), zap.Error(err))
		return nil
	}
	return cards
}

func (h *Handler) transactionsForUser(userID string) []uiTransaction {
	transactions, err := h.store.ListTransactionsByUser(userID)
	if err != nil {
		logger.Warn("failed to load transactions for admin ui", zap.String("user_id", userID), zap.Error(err))
		return nil
	}

	out := make([]uiTransaction, 0, len(transactions))
	for _, tx := range transactions {
		out = append(out, uiTransaction{
			Transaction: tx,
			StatusName:  transactionStatusName(tx.Status),
			CanSettle: tx.DepositType == consts.DepositTypeWithdrawal &&
				tx.Status == consts.TransactionStatusPending,
		})
	}
	return out
}

// UIKYCForm renders the KYC event form.
// GET /ui/actions/kyc
func (h *Handler) UIKYCForm(w http.ResponseWriter, r *http.Request) {
	flash, isError := flashFrom(r)
	h.renderUI(w, "kyc", uiView{
		Title:          "KYC event",
		Flash:          flash,
		FlashIsError:   isError,
		Users:          h.userSummaries(),
		Outcomes:       kycOutcomeOptions(),
		SelectedUserID: r.URL.Query().Get("userID"),
	})
}

// UIKYCAction sends a verification event for a user.
// POST /ui/actions/kyc
func (h *Handler) UIKYCAction(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.FormValue("userID"))
	outcome := strings.TrimSpace(r.FormValue("outcome"))
	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" {
		message = "Sent from the MockGatehub admin UI"
	}

	if userID == "" || outcome == "" {
		h.redirectWithFlash(w, r, "/ui/actions/kyc", "Choose a user and an outcome", true)
		return
	}

	user, err := h.store.GetUser(userID)
	if err != nil {
		h.redirectWithFlash(w, r, "/ui/actions/kyc", "User not found", true)
		return
	}

	event, err := h.applyUIKYCOutcome(user, outcome)
	if err != nil {
		h.redirectWithFlash(w, r, "/ui/actions/kyc", err.Error(), true)
		return
	}

	h.webhookManager.SendAsync(event, userID, map[string]interface{}{"message": message}, 0)

	logger.Info("admin ui sent kyc event",
		zap.String("user_id", userID),
		zap.String("outcome", outcome),
		zap.String("event", event),
	)
	h.redirectWithFlash(w, r, "/ui/users/"+userID, "Sent "+event, false)
}

// applyUIKYCOutcome resolves a form selection to an event, updating the stored
// state for the verification outcomes. A document notice is not a verification
// outcome, so it is announced without moving the user's state.
func (h *Handler) applyUIKYCOutcome(user *models.User, outcome string) (string, error) {
	switch outcome {
	case consts.WebhookEventDocumentNoticeExpired, consts.WebhookEventDocumentNoticeWarning:
		return outcome, nil
	}

	event, _, err := applyKYCOutcome(user, outcome)
	if err != nil {
		return "", err
	}

	// A user the provider has ruled on always carries a risk level. The iframe
	// flow defaults it the same way, so a user verified through the UI is not
	// left in a state the iframe could never produce.
	if user.RiskLevel == "" {
		user.RiskLevel = consts.RiskLevelLow
	}

	if err := h.store.UpdateUser(user); err != nil {
		logger.Error("failed to update user from admin ui", zap.String("user_id", user.ID), zap.Error(err))
		return "", errFailedToUpdateUser
	}
	return event, nil
}

// UICardTxForm renders the card transaction simulation form.
// GET /ui/actions/card-transaction
func (h *Handler) UICardTxForm(w http.ResponseWriter, r *http.Request) {
	selectedUser := r.URL.Query().Get("userID")
	flash, isError := flashFrom(r)

	view := uiView{
		Title:          "Card transaction",
		Flash:          flash,
		FlashIsError:   isError,
		Users:          h.userSummaries(),
		Scenarios:      listCardScenarios(),
		Events:         cardTxEventOptions(),
		SelectedUserID: selectedUser,
		SelectedCardID: r.URL.Query().Get("cardID"),
	}
	if selectedUser != "" {
		view.Cards = h.cardsForUser(selectedUser)
	}

	h.renderUI(w, "cardTx", view)
}

// UICardTxAction simulates a card transaction from the form selection. It goes
// through the same code path as the simulation endpoint, so the UI and the API
// cannot drift apart.
// POST /ui/actions/card-transaction
func (h *Handler) UICardTxAction(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.FormValue("userID"))
	cardID := strings.TrimSpace(r.FormValue("cardID"))
	scenario := strings.TrimSpace(r.FormValue("scenario"))

	if userID == "" || cardID == "" || scenario == "" {
		h.redirectWithFlash(w, r, "/ui/actions/card-transaction", "Choose a user, a card and a scenario", true)
		return
	}

	overrides := map[string]interface{}{}
	if amount := strings.TrimSpace(r.FormValue("amount")); amount != "" {
		// Both figures move together; a transaction whose billing amount
		// disagreed with its transaction amount would be incoherent.
		overrides["transactionAmount"] = amount
		overrides["billingAmount"] = amount
	}
	if merchant := strings.TrimSpace(r.FormValue("merchantName")); merchant != "" {
		overrides["merchantName"] = merchant
	}

	result, err := h.simulateCardTransaction(SimulateCardTransactionRequest{
		UserID:    userID,
		CardID:    cardID,
		Scenario:  scenario,
		Overrides: overrides,
		Event:     strings.TrimSpace(r.FormValue("event")),
	})
	if err != nil {
		h.redirectWithFlash(w, r, "/ui/actions/card-transaction", err.Error(), true)
		return
	}

	h.redirectWithFlash(w, r, "/ui/users/"+userID,
		"Simulated "+result.Scenario+" and sent "+result.Event, false)
}

// UIWithdrawalSettle settles a pending withdrawal from the user detail view.
// POST /ui/actions/withdrawal/settle
func (h *Handler) UIWithdrawalSettle(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.FormValue("userID"))
	txID := strings.TrimSpace(r.FormValue("txID"))
	event := strings.TrimSpace(r.FormValue("event"))

	redirect := "/ui"
	if userID != "" {
		redirect = "/ui/users/" + userID
	}

	if txID == "" || event == "" {
		h.redirectWithFlash(w, r, redirect, "A transaction and an event are required", true)
		return
	}

	if err := h.settleWithdrawal(txID, event); err != nil {
		h.redirectWithFlash(w, r, redirect, err.Error(), true)
		return
	}

	h.redirectWithFlash(w, r, redirect, "Sent "+event, false)
}
