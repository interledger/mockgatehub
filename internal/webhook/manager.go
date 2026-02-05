package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mockgatehub/internal/auth"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/utils"

	"go.uber.org/zap"
)

// Manager handles webhook delivery
type Manager struct {
	webhookURL    string
	webhookSecret string
	httpClient    *http.Client
	queue         *Queue // Redis-backed job queue
}

// WebhookPayload represents the webhook request body (matches wallet-backend IWebhookData)
type WebhookPayload struct {
	UUID        string                 `json:"uuid"`        // Webhook UUID (required by controller check)
	Timestamp   string                 `json:"timestamp"`   // Milliseconds since epoch as string (e.g., "1768920404045")
	EventType   string                 `json:"event_type"`  // e.g., "core.deposit.completed"
	UserUUID    string                 `json:"user_uuid"`   // GateHub user UUID
	Environment string                 `json:"environment"` // "sandbox" or "production"
	Data        map[string]interface{} `json:"data"`        // Event-specific data (IDepositWebhookData, etc.)
}

// NewManager creates a new webhook manager
func NewManager(webhookURL, webhookSecret string, queue *Queue) *Manager {
	logger.Info("initializing webhook manager",
		zap.String("url", webhookURL),
		zap.Int("secret_length", len(webhookSecret)),
	)

	return &Manager{
		webhookURL:    webhookURL,
		webhookSecret: webhookSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		queue: queue,
	}
}

// SendAsync enqueues a webhook job for asynchronous delivery
func (m *Manager) SendAsync(eventType, userID string, data any) {
	if m.webhookURL == "" {
		logger.Info("skipping webhook send - no url configured", zap.String("event", eventType), zap.String("user", userID))
		return
	}

	logger.Info("enqueueing webhook", zap.String("event", eventType), zap.String("user", userID))

	ctx := context.Background()
	jobID, err := m.queue.Enqueue(ctx, eventType, userID, data)
	if err != nil {
		logger.Error("failed to enqueue webhook", zap.Error(err))
		return
	}

	logger.Info("webhook enqueued successfully", zap.String("job_id", jobID))
}

// HasURL reports whether a webhook URL is configured.
func (m *Manager) HasURL() bool {
	return m != nil && m.webhookURL != ""
}

// send is now public (called by worker) and performs a single send attempt
// Worker handles retry logic via queue rescheduling

// Send performs the actual HTTP webhook request (called by worker)
func (m *Manager) send(eventType, userID string, data any) error {
	normalized := normalizeVerificationPayload(eventType, data)

	// Build payload - testnet wallet-backend expects timestamp as milliseconds string
	now := time.Now()
	payload := WebhookPayload{
		UUID:        utils.GenerateUUID(),               // Generate unique webhook UUID
		Timestamp:   fmt.Sprintf("%d", now.UnixMilli()), // Milliseconds since epoch as string
		EventType:   eventType,                          // e.g., "core.deposit.completed"
		UserUUID:    userID,                             // GateHub user UUID
		Environment: "sandbox",                          // Always sandbox for mockgatehub
		Data:        normalized,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", m.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")

	// Generate signature - GateHub expects SHA256(body) signed with the secret
	// The signature should use the entire JSON payload as the message
	signature := auth.GenerateGateHubWebhookSignature(string(body), m.webhookSecret)
	req.Header.Set("X-GH-Webhook-Signature", signature)

	logger.Info("sending webhook request",
		zap.String("url", m.webhookURL),
		zap.String("event_type", eventType),
		zap.String("user_id", userID),
		zap.String("signature", signature),
	)

	// Send request
	start := time.Now()
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)
	logger.Info("webhook response received",
		zap.Duration("duration", duration),
		zap.Int("status_code", resp.StatusCode),
	)

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code: %d %s", resp.StatusCode, resp.Status)
	}

	return nil
}

func normalizeVerificationPayload(eventType string, data any) map[string]interface{} {
	converted := coerceToMap(data)

	switch eventType {
	case "id.verification.accepted", "id.verification.rejected", "id.verification.action_required":
		if _, ok := converted["gateway"]; !ok {
			converted["gateway"] = "paywiser"
		}
		if _, ok := converted["verified"]; !ok {
			short := "action_required"
			status := 0
			switch eventType {
			case "id.verification.accepted":
				short = "accepted"
				status = 1
			case "id.verification.rejected":
				short = "rejected"
				status = 2
			}
			converted["verified"] = map[string]interface{}{
				"short":  short,
				"status": status,
			}
		}
	}

	return converted
}

func coerceToMap(data any) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}

	if typed, ok := data.(map[string]interface{}); ok {
		return typed
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return map[string]interface{}{}
	}

	var converted map[string]interface{}
	if err := json.Unmarshal(bytes, &converted); err != nil {
		return map[string]interface{}{}
	}

	return converted
}
