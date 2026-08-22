package webhook

import (
	"testing"

	"mockgatehub/internal/consts"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeVerificationPayload_SummarisesTheThreeVerdicts(t *testing.T) {
	// Consumers read data.verified.status to decide whether the user passed.
	cases := []struct {
		event      string
		wantShort  string
		wantStatus int
	}{
		{consts.WebhookEventKYCAccepted, "accepted", 1},
		{consts.WebhookEventKYCRejected, "rejected", 2},
		{consts.WebhookEventKYCActionRequired, "action_required", 0},
	}

	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			out := normalizeVerificationPayload(tc.event, map[string]interface{}{"message": "hello"})

			verified, ok := out["verified"].(map[string]interface{})
			require.True(t, ok, "expected a verified summary, got %v", out["verified"])
			assert.Equal(t, tc.wantShort, verified["short"])
			assert.Equal(t, tc.wantStatus, verified["status"])
			assert.Equal(t, "paywiser", out["gateway"])
			assert.Equal(t, "hello", out["message"], "the caller's data must survive")
		})
	}
}

func TestNormalizeVerificationPayload_DoesNotInventAVerdictForNonOutcomes(t *testing.T) {
	// A resubmission request or a document notice is not a verdict. Attaching a
	// "verified" summary would tell the consumer verification had concluded.
	for _, event := range []string{
		consts.WebhookEventKYCResubmission,
		consts.WebhookEventDocumentNoticeExpired,
		consts.WebhookEventDocumentNoticeWarning,
	} {
		t.Run(event, func(t *testing.T) {
			out := normalizeVerificationPayload(event, map[string]interface{}{"message": "documents needed"})

			assert.NotContains(t, out, "verified",
				"%s must not carry a synthesised verification verdict", event)
			// These events still describe an identity gateway interaction.
			assert.Equal(t, "paywiser", out["gateway"])
			assert.Equal(t, "documents needed", out["message"])
		})
	}
}

func TestNormalizeVerificationPayload_KeepsACallerSuppliedVerdict(t *testing.T) {
	supplied := map[string]interface{}{"short": "custom", "status": 7}
	out := normalizeVerificationPayload(consts.WebhookEventKYCAccepted, map[string]interface{}{
		"verified": supplied,
		"gateway":  "someone-else",
	})

	assert.Equal(t, supplied, out["verified"], "an explicit verdict must not be overwritten")
	assert.Equal(t, "someone-else", out["gateway"])
}

func TestNormalizeVerificationPayload_LeavesUnrelatedEventsAlone(t *testing.T) {
	// A deposit or card event must not acquire identity fields.
	for _, event := range []string{
		consts.WebhookEventDepositCompleted,
		consts.WebhookEventCardTransactionAuthorization,
		consts.WebhookEventCardCreated,
	} {
		t.Run(event, func(t *testing.T) {
			out := normalizeVerificationPayload(event, map[string]interface{}{"amount": "10.00"})

			assert.NotContains(t, out, "gateway")
			assert.NotContains(t, out, "verified")
			assert.Equal(t, "10.00", out["amount"])
		})
	}
}

func TestIsVerificationOutcome(t *testing.T) {
	for _, event := range []string{
		consts.WebhookEventKYCAccepted,
		consts.WebhookEventKYCRejected,
		consts.WebhookEventKYCActionRequired,
	} {
		assert.True(t, isVerificationOutcome(event), event)
	}

	for _, event := range []string{
		consts.WebhookEventKYCResubmission,
		consts.WebhookEventDocumentNoticeExpired,
		consts.WebhookEventDocumentNoticeWarning,
		consts.WebhookEventDepositCompleted,
		"",
	} {
		assert.False(t, isVerificationOutcome(event), event)
	}
}
