package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"mockgatehub/internal/auth"
	"mockgatehub/internal/logger"

	"go.uber.org/zap"
)

// receivedWebhookLimit bounds the sink so a long-running instance cannot grow
// without limit. Oldest entries are dropped first.
const receivedWebhookLimit = 500

// ReceivedWebhook is one delivery the sink accepted.
type ReceivedWebhook struct {
	ReceivedAt time.Time `json:"received_at"`
	EventType  string    `json:"event_type"`
	UserUUID   string    `json:"user_uuid"`
	// SignatureValid records whether the delivery carried a signature matching
	// the configured webhook secret. Exposed so a test can assert that
	// webhooks are signed, not merely that they arrived.
	SignatureValid bool                   `json:"signature_valid"`
	Payload        map[string]interface{} `json:"payload"`
	Body           string                 `json:"body"`
}

// webhookSink records webhooks delivered back to this service.
//
// This exists purely so that automated tests can assert on webhook behaviour.
// A consumer that wants webhooks points WEBHOOK_URL at its own endpoint; a test
// harness with nowhere to receive them points it at this one instead.
type webhookSink struct {
	mu       sync.RWMutex
	received []ReceivedWebhook
}

var sink webhookSink

func (s *webhookSink) record(entry ReceivedWebhook) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.received = append(s.received, entry)
	if len(s.received) > receivedWebhookLimit {
		s.received = s.received[len(s.received)-receivedWebhookLimit:]
	}
}

func (s *webhookSink) list() []ReceivedWebhook {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ReceivedWebhook, len(s.received))
	copy(out, s.received)
	return out
}

func (s *webhookSink) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = nil
}

// TestWebhookSink accepts a webhook delivery and records it.
// POST /test-webhook
func (h *Handler) TestWebhookSink(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "could not read body")
		return
	}

	entry := ReceivedWebhook{
		ReceivedAt: time.Now().UTC(),
		Body:       string(body),
	}

	// Verify the signature rather than just recording it, so a test can tell a
	// correctly signed delivery from an unsigned one.
	expected := auth.GenerateGateHubWebhookSignature(string(body), h.config.WebhookSecret)
	entry.SignatureValid = r.Header.Get("X-GH-Webhook-Signature") == expected

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		entry.Payload = payload
		entry.EventType, _ = payload["event_type"].(string)
		entry.UserUUID, _ = payload["user_uuid"].(string)
	}

	sink.record(entry)

	logger.Debug("test webhook sink received delivery",
		zap.String("event_type", entry.EventType),
		zap.String("user_uuid", entry.UserUUID),
		zap.Bool("signature_valid", entry.SignatureValid),
	)

	// Acknowledge, or the webhook worker will keep retrying.
	h.sendJSON(w, http.StatusOK, map[string]string{"status": "received"})
}

// ListReceivedWebhooks returns recorded deliveries, newest last, optionally
// filtered by event type and user.
// GET /admin/received-webhooks?event=...&user=...
func (h *Handler) ListReceivedWebhooks(w http.ResponseWriter, r *http.Request) {
	eventFilter := r.URL.Query().Get("event")
	userFilter := r.URL.Query().Get("user")

	matches := make([]ReceivedWebhook, 0)
	for _, entry := range sink.list() {
		if eventFilter != "" && entry.EventType != eventFilter {
			continue
		}
		if userFilter != "" && entry.UserUUID != userFilter {
			continue
		}
		matches = append(matches, entry)
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"webhooks": matches,
		"count":    len(matches),
	})
}

// ClearReceivedWebhooks empties the sink so one scenario's deliveries do not
// leak into the next.
// DELETE /admin/received-webhooks
func (h *Handler) ClearReceivedWebhooks(w http.ResponseWriter, r *http.Request) {
	sink.clear()
	h.sendJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
