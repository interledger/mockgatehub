package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mockgatehub/internal/auth"
	"mockgatehub/internal/consts"
	"mockgatehub/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Sumsub verification mapping ----------

func TestSumsubVerificationStatusState_MapsEachKYCState(t *testing.T) {
	// Consumers branch on these numbers to decide whether a user is verified,
	// so a state must never be reported as a verdict it did not reach.
	cases := []struct {
		kycState    string
		wantStatus  int
		wantState   int
		description string
	}{
		{consts.KYCStateAccepted, 1, 1, "accepted is the only verified combination"},
		{consts.KYCStateRejected, 2, 0, "rejected is a verdict but not a pass"},
		{consts.KYCStateResubmission, 10, 0, "resubmission is distinguishable from every other state"},
		{consts.KYCStateActionRequired, 0, 0, "action_required is still in progress"},
		{"", 0, 0, "an unset state must not look verified"},
		{"something-new", 0, 0, "an unrecognised state must not look verified"},
	}

	for _, tc := range cases {
		t.Run(tc.kycState, func(t *testing.T) {
			status, state := sumsubVerificationStatusState(&models.User{KYCState: tc.kycState})
			assert.Equal(t, tc.wantStatus, status, tc.description)
			assert.Equal(t, tc.wantState, state, tc.description)
		})
	}
}

func TestGetUser_ReportsVerificationConsistentWithKYCState(t *testing.T) {
	h, store := newSeededHandler(t)

	for _, kycState := range []string{
		consts.KYCStateAccepted, consts.KYCStateRejected,
		consts.KYCStateResubmission, consts.KYCStateActionRequired,
	} {
		t.Run(kycState, func(t *testing.T) {
			user, err := store.GetUser(consts.TestUser1ID)
			require.NoError(t, err)
			user.KYCState = kycState
			require.NoError(t, store.UpdateUser(user))

			body := getUserResponse(t, h, consts.TestUser1ID)

			assert.Equal(t, kycState, body.KYCState)
			require.Len(t, body.Verifications, 1)

			wantStatus, wantState := sumsubVerificationStatusState(user)
			assert.Equal(t, wantStatus, body.Verifications[0].Status)
			assert.Equal(t, wantState, body.Verifications[0].State)
			assert.Equal(t, "Sumsub", body.Verifications[0].Provider)
			assert.Equal(t, "sumsub", body.Verifications[0].ProviderType)
		})
	}
}

func TestGetUser_EmitsBothIdAndUuid(t *testing.T) {
	// The provider names this field "uuid"; this service has always emitted
	// "id". Dropping either would break one spelling of consumer.
	h, _ := newSeededHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/id/v1/users/"+consts.TestUser1ID, nil)
	req = withChiParams(req, map[string]string{"userID": consts.TestUser1ID})
	rr := httptest.NewRecorder()
	h.GetUser(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &raw))
	assert.Equal(t, consts.TestUser1ID, raw["id"])
	assert.Equal(t, consts.TestUser1ID, raw["uuid"])
}

func TestGetUser_ReportsProfileCreationDisabledFlag(t *testing.T) {
	h, store := newSeededHandler(t)

	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	user.IsProfileCreationDisabled = true
	require.NoError(t, store.UpdateUser(user))

	body := getUserResponse(t, h, consts.TestUser1ID)
	assert.True(t, body.IsProfileCreationDisabled)
}

func getUserResponse(t *testing.T, h *Handler, userID string) models.GetUserResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/id/v1/users/"+userID, nil)
	req = withChiParams(req, map[string]string{"userID": userID})
	rr := httptest.NewRecorder()
	h.GetUser(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	var body models.GetUserResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	return body
}

// ---------- KYC outcome selection ----------

func TestApplyKYCOutcome_SetsStateAndPairsTheRightEvent(t *testing.T) {
	cases := []struct {
		outcome   string
		wantState string
		wantEvent string
	}{
		{consts.KYCStateAccepted, consts.KYCStateAccepted, consts.WebhookEventKYCAccepted},
		{consts.KYCStateRejected, consts.KYCStateRejected, consts.WebhookEventKYCRejected},
		{consts.KYCStateActionRequired, consts.KYCStateActionRequired, consts.WebhookEventKYCActionRequired},
		{consts.KYCStateResubmission, consts.KYCStateResubmission, consts.WebhookEventKYCResubmission},
	}

	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			user := &models.User{}
			event, message, err := applyKYCOutcome(user, tc.outcome)
			require.NoError(t, err)

			assert.Equal(t, tc.wantState, user.KYCState)
			assert.Equal(t, tc.wantEvent, event)
			assert.NotEmpty(t, message, "the response message is shown to a user")
		})
	}
}

func TestApplyKYCOutcome_RejectsAnUnknownOutcome(t *testing.T) {
	// Silently defaulting to accepted would turn a typo into a passing KYC.
	user := &models.User{KYCState: consts.KYCStateActionRequired}
	_, _, err := applyKYCOutcome(user, "approved")

	require.Error(t, err)
	assert.Equal(t, consts.KYCStateActionRequired, user.KYCState, "the state must be left alone")
}

func TestShouldSkipAcceptedWebhook_OnlySuppressesAfterAResubmission(t *testing.T) {
	assert.True(t, shouldSkipAcceptedWebhook(consts.KYCStateResubmission, consts.WebhookEventKYCAccepted),
		"the consumer drives a resubmitted user onward itself")

	assert.False(t, shouldSkipAcceptedWebhook(consts.KYCStateActionRequired, consts.WebhookEventKYCAccepted))
	assert.False(t, shouldSkipAcceptedWebhook("", consts.WebhookEventKYCAccepted))
	assert.False(t, shouldSkipAcceptedWebhook(consts.KYCStateResubmission, consts.WebhookEventKYCRejected),
		"a rejection after a resubmission is still news")
}

// submitKYCForm posts the iframe KYC form for a user.
func submitKYCForm(t *testing.T, h *Handler, userID string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("user_id", userID)
	form.Set("first_name", "Jane")
	form.Set("last_name", "Smith")
	for k, v := range extra {
		form.Set(k, v)
	}

	req := httptest.NewRequest(http.MethodPost, "/iframe/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.KYCIframeSubmit(rr, req)
	return rr
}

func TestKYCIframeSubmit_DefaultsToAccepted(t *testing.T) {
	// Every existing caller omits kyc_outcome and expects acceptance.
	h, store := newSeededHandler(t)

	rr := submitKYCForm(t, h, consts.TestUser1ID, nil)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, consts.KYCStateAccepted, resp["status"])

	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateAccepted, user.KYCState)
}

func TestKYCIframeSubmit_HonoursTheRequestedOutcome(t *testing.T) {
	for _, outcome := range []string{
		consts.KYCStateAccepted, consts.KYCStateRejected,
		consts.KYCStateActionRequired, consts.KYCStateResubmission,
	} {
		t.Run(outcome, func(t *testing.T) {
			h, store := newSeededHandler(t)

			rr := submitKYCForm(t, h, consts.TestUser1ID, map[string]string{"kyc_outcome": outcome})
			require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

			var resp map[string]string
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
			assert.Equal(t, outcome, resp["status"])

			// The persisted state is what a later read reports.
			user, err := store.GetUser(consts.TestUser1ID)
			require.NoError(t, err)
			assert.Equal(t, outcome, user.KYCState)
		})
	}
}

func TestKYCIframeSubmit_RejectsAnUnknownOutcomeWithoutChangingState(t *testing.T) {
	h, store := newSeededHandler(t)

	before, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	stateBefore := before.KYCState

	rr := submitKYCForm(t, h, consts.TestUser1ID, map[string]string{"kyc_outcome": "definitely-verified"})
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	after, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, stateBefore, after.KYCState)
}

func TestKYCIframeSubmit_StillRecordsTheProfile(t *testing.T) {
	// A rejection must not discard the details the user supplied.
	h, store := newSeededHandler(t)

	rr := submitKYCForm(t, h, consts.TestUser1ID, map[string]string{
		"kyc_outcome": consts.KYCStateRejected,
		"first_name":  "Ada",
		"last_name":   "Lovelace",
	})
	require.Equal(t, http.StatusOK, rr.Code)

	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, "Ada", user.FirstName)
	assert.Equal(t, "Lovelace", user.LastName)
}

// ---------- Admin KYC state override ----------

func setUserKYCState(t *testing.T, h *Handler, userID string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/admin/users/"+userID+"/kyc-state", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"userID": userID})

	rr := httptest.NewRecorder()
	h.SetUserKYCStateQuiet(rr, req)
	return rr
}

// TestSetUserKYCStateQuiet_MovesTheState checks the state change lands and is
// visible on a subsequent read. That the change emits no webhook is asserted
// end-to-end, where a real sink can observe deliveries.
func TestSetUserKYCStateQuiet_MovesTheState(t *testing.T) {
	h, store := newSeededHandler(t)

	rr := setUserKYCState(t, h, consts.TestUser1ID, map[string]string{
		"kyc_state": consts.KYCStateResubmission,
	})
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, consts.KYCStateResubmission, user.KYCState)

	// And the new state must be what a read reports.
	body := getUserResponse(t, h, consts.TestUser1ID)
	assert.Equal(t, consts.KYCStateResubmission, body.KYCState)
	assert.Equal(t, 10, body.Verifications[0].Status)
}

func TestSetUserKYCStateQuiet_CanAlsoSetRiskLevel(t *testing.T) {
	h, store := newSeededHandler(t)

	rr := setUserKYCState(t, h, consts.TestUser1ID, map[string]string{
		"kyc_state":  consts.KYCStateAccepted,
		"risk_level": consts.RiskLevelHigh,
	})
	require.Equal(t, http.StatusOK, rr.Code)

	user, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, consts.RiskLevelHigh, user.RiskLevel)
}

func TestSetUserKYCStateQuiet_LeavesRiskLevelAloneWhenOmitted(t *testing.T) {
	h, store := newSeededHandler(t)

	before, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	riskBefore := before.RiskLevel

	rr := setUserKYCState(t, h, consts.TestUser1ID, map[string]string{"kyc_state": consts.KYCStateRejected})
	require.Equal(t, http.StatusOK, rr.Code)

	after, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)
	assert.Equal(t, riskBefore, after.RiskLevel)
}

func TestSetUserKYCStateQuiet_RejectsBadInput(t *testing.T) {
	h, store := newSeededHandler(t)

	before, err := store.GetUser(consts.TestUser1ID)
	require.NoError(t, err)

	t.Run("unknown state", func(t *testing.T) {
		rr := setUserKYCState(t, h, consts.TestUser1ID, map[string]string{"kyc_state": "verified-ish"})
		assert.Equal(t, http.StatusBadRequest, rr.Code)

		after, err := store.GetUser(consts.TestUser1ID)
		require.NoError(t, err)
		assert.Equal(t, before.KYCState, after.KYCState)
	})

	t.Run("missing state", func(t *testing.T) {
		rr := setUserKYCState(t, h, consts.TestUser1ID, map[string]string{})
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("unknown user", func(t *testing.T) {
		rr := setUserKYCState(t, h, "no-such-user", map[string]string{"kyc_state": consts.KYCStateAccepted})
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

// ---------- Signed 2FA callback ----------

func TestCall2FAVerify_SignsTheRequest(t *testing.T) {
	// The receiver has no other way to tell a genuine verification request from
	// any other POST to the same URL.
	var gotSignature, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotSignature = r.Header.Get("X-GH-Webhook-Signature")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	h, _ := newSeededHandler(t)
	h.config.WebhookSecret = "the-shared-secret"

	ok, err := h.call2FAVerify(server.URL, "123456")
	require.NoError(t, err)
	assert.True(t, ok)

	require.NotEmpty(t, gotSignature, "the callback must be signed")
	assert.Equal(t,
		auth.GenerateGateHubWebhookSignature(gotBody, "the-shared-secret"),
		gotSignature,
		"the signature must be computed over the exact body sent")
}

func TestCall2FAVerify_ReportsTheIntegratorsVerdict(t *testing.T) {
	for _, verdict := range []bool{true, false} {
		t.Run(map[bool]string{true: "accepted", false: "declined"}[verdict], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp, _ := json.Marshal(map[string]bool{"success": verdict})
				_, _ = w.Write(resp)
			}))
			defer server.Close()

			h, _ := newSeededHandler(t)
			ok, err := h.call2FAVerify(server.URL, "123456")
			require.NoError(t, err)
			assert.Equal(t, verdict, ok)
		})
	}
}

func TestCall2FAVerify_TreatsAnErrorStatusAsAFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	h, _ := newSeededHandler(t)
	ok, err := h.call2FAVerify(server.URL, "123456")

	assert.Error(t, err, "a failed callback must not read as a successful verification")
	assert.False(t, ok)
}
