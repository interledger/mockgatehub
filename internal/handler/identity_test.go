package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setup2FATestHandler creates a handler with a managed user and optional organization config.
// It returns the handler, the store, and the user ID.
func setup2FATestHandler(t *testing.T, orgAPIBaseURL string) (*Handler, *storage.MemoryStorage, string) {
	t.Helper()
	store := storage.NewMemoryStorage()
	wm := webhook.NewManager("", "test-secret", nil, store, "default-org")
	h := NewHandler(store, wm)

	// Create a test user
	user := &models.User{
		ID:        "test-user-001",
		Email:     "kycuser@example.com",
		Managed:   true,
		Activated: true,
		KYCState:  consts.KYCStateActionRequired,
		RiskLevel: consts.RiskLevelLow,
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.CreateUser(user))

	// Map a bearer token to this user
	h.tokenToUser.Store("kyc-token-test", "test-user-001")

	// Create default organization if apiBaseUrl is provided
	if orgAPIBaseURL != "" {
		org := &models.Organization{
			ID:         "default-org",
			APIBaseURL: orgAPIBaseURL,
			TwoFAType:  "totp",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, store.CreateOrganization(org))
	}

	return h, store, user.ID
}

// postKYCForm sends a form POST to KYCIframeSubmit with given form values.
func postKYCForm(h *Handler, formValues url.Values) *httptest.ResponseRecorder {
	body := formValues.Encode()
	req := httptest.NewRequest(http.MethodPost, "/iframe/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.KYCIframeSubmit(w, req)
	return w
}

func baseKYCForm(userID string) url.Values {
	return url.Values{
		"user_id":    {userID},
		"first_name": {"Jane"},
		"last_name":  {"Doe"},
		"dob":        {"1990-05-15"},
		"address":    {"123 Main St"},
		"city":       {"Testville"},
		"country":    {"US"},
		"risk_level": {"low"},
	}
}

func TestKYCIframeSubmit_Without2FA_NormalFlow(t *testing.T) {
	h, store, userID := setup2FATestHandler(t, "")

	form := baseKYCForm(userID)
	// No trigger_2fa field — normal flow
	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify user KYC state is accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateAccepted, user.KYCState)
}

func TestKYCIframeSubmit_2FA_Success(t *testing.T) {
	// Create a mock integrator 2FA endpoint that returns success
	var receivedBody map[string]interface{}
	var receivedPath string
	mock2FA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		bodyBytes, _ := io.ReadAll(r.Body)
		json.Unmarshal(bodyBytes, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))
	defer mock2FA.Close()

	h, store, userID := setup2FATestHandler(t, mock2FA.URL)

	form := baseKYCForm(userID)
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "123456")

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify 2FA callback was made with correct payload
	assert.Equal(t, fmt.Sprintf("/v1/users/managed/%s/2fa", userID), receivedPath)
	assert.Equal(t, "VERIFY", receivedBody["action"])
	assert.Equal(t, "123456", receivedBody["code"])

	// Verify user KYC state is accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateAccepted, user.KYCState)
}

func TestKYCIframeSubmit_2FA_Rejected(t *testing.T) {
	// Mock integrator returns success: false
	mock2FA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": false})
	}))
	defer mock2FA.Close()

	h, store, userID := setup2FATestHandler(t, mock2FA.URL)

	form := baseKYCForm(userID)
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "wrong-code")

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Verify KYC state is NOT accepted (still action_required)
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateActionRequired, user.KYCState)
}

func TestKYCIframeSubmit_2FA_IntegratorNon2xx(t *testing.T) {
	// Mock integrator returns 500
	mock2FA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer mock2FA.Close()

	h, store, userID := setup2FATestHandler(t, mock2FA.URL)

	form := baseKYCForm(userID)
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "123456")

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Verify KYC state is NOT accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateActionRequired, user.KYCState)
}

func TestKYCIframeSubmit_2FA_NoOrgCallbackURL(t *testing.T) {
	// No organization configured (empty orgAPIBaseURL)
	h, store, userID := setup2FATestHandler(t, "")

	form := baseKYCForm(userID)
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "123456")

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Verify error message mentions callback URL
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	msg, _ := resp["message"].(string)
	assert.Contains(t, msg, "no organization callback URL configured")

	// Verify KYC state is NOT accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateActionRequired, user.KYCState)
}

func TestKYCIframeSubmit_2FA_EmptyCode(t *testing.T) {
	// 2FA checkbox checked, empty TOTP code — callback should still be made (no client-side validation)
	var receivedBody map[string]interface{}
	mock2FA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		json.Unmarshal(bodyBytes, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))
	defer mock2FA.Close()

	h, store, userID := setup2FATestHandler(t, mock2FA.URL)

	form := baseKYCForm(userID)
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "") // empty code

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify callback was still made with empty code
	assert.Equal(t, "VERIFY", receivedBody["action"])
	assert.Equal(t, "", receivedBody["code"])

	// Verify KYC accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateAccepted, user.KYCState)
}

func TestKYCIframeSubmit_2FA_NetworkError(t *testing.T) {
	// Use a URL that will fail to connect
	h, store, userID := setup2FATestHandler(t, "https://127.0.0.1:1") // unroutable port

	form := baseKYCForm(userID)
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "123456")

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Verify KYC state is NOT accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateActionRequired, user.KYCState)
}

func TestKYCIframeSubmit_2FA_TokenResolvedUser(t *testing.T) {
	// 2FA with user resolved from token (no user_id in form)
	mock2FA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))
	defer mock2FA.Close()

	h, store, userID := setup2FATestHandler(t, mock2FA.URL)

	form := baseKYCForm("")
	form.Del("user_id")
	form.Set("token", "kyc-token-test") // maps to test-user-001
	form.Set("trigger_2fa", "on")
	form.Set("totp_code", "654321")

	w := postKYCForm(h, form)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify user KYC state is accepted
	user, err := store.GetUser(userID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateAccepted, user.KYCState)
}
