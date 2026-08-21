package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"mockgatehub/internal/logger"
	"mockgatehub/internal/models"

	"go.uber.org/zap"
)

// Helper methods for JSON responses

func (h *Handler) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	// Marshal before writing the header. Writing the status first means a
	// marshal failure can only append an error body to an already-committed
	// 200, so the caller sees a success status with an error payload.
	respStatus := status
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logger.Error("failed to marshal response", zap.Error(err))
		respStatus = http.StatusInternalServerError
		body = []byte(`{"error":"internal server error"}`)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(respStatus)

	logger.Debug("sending json response", zap.Int("status", respStatus))
	if _, err := w.Write(body); err != nil {
		logger.Warn("failed to write json response", zap.Error(err))
	}
}

// formatAmount renders a monetary amount the way the GateHub API does: a
// fixed two-decimal string rather than a JSON number.
func formatAmount(amount float64) string {
	return fmt.Sprintf("%.2f", amount)
}

// tokenPrefix returns a leading slice of a token, short enough to correlate log
// lines without writing a usable credential to the log. It is safe for tokens
// shorter than the prefix length, which hand-rolled slicing at the call sites
// was not.
func tokenPrefix(token string) string {
	const prefixLen = 20
	if len(token) > prefixLen {
		return token[:prefixLen]
	}
	return token
}

func (h *Handler) sendError(w http.ResponseWriter, status int, message string) {
	logger.Error("sending error response", zap.Int("status", status), zap.String("message", message))
	h.sendJSON(w, status, models.ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	})
}

// setCORSHeaders adds CORS headers to the response writer
func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// sendJSONWithCORS sends a JSON response with CORS headers
func (h *Handler) sendJSONWithCORS(w http.ResponseWriter, status int, data interface{}) {
	setCORSHeaders(w)
	h.sendJSON(w, status, data)
}

// sendErrorWithCORS sends an error response with CORS headers
func (h *Handler) sendErrorWithCORS(w http.ResponseWriter, status int, message string) {
	setCORSHeaders(w)
	h.sendError(w, status, message)
}

func (h *Handler) decodeJSON(r *http.Request, v interface{}) error {
	// Read body for logging
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}

	// Log the raw request body
	logger.Debug("received request body", zap.String("body", string(body)))

	// Restore body for decoding
	r.Body = io.NopCloser(bytes.NewReader(body))

	// Decode
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		logger.Error("failed to decode json", zap.Error(err))
		return err
	}

	// Log the decoded structure
	pretty, _ := json.MarshalIndent(v, "", "  ")
	logger.Debug("decoded request", zap.String("data", string(pretty)))

	return nil
}
