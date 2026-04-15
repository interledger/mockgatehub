package webhook

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamJob_ToStreamFields_Roundtrip(t *testing.T) {
	original := StreamJob{
		JobID:     "job-1",
		EventType: "core.deposit.completed",
		UserID:    "user-abc",
		Data:      map[string]interface{}{"amount": "100.00"},
		Attempts:  2,
		CreatedAt: "2025-01-01T00:00:00Z",
	}

	fields, err := original.toStreamFields()
	require.NoError(t, err)
	assert.Equal(t, "job-1", fields["job_id"])
	assert.Equal(t, "core.deposit.completed", fields["event_type"])
	assert.Equal(t, "user-abc", fields["user_id"])
	assert.Equal(t, "2", fields["attempts"])

	// Simulate a Redis stream message roundtrip
	values := make(map[string]interface{})
	for k, v := range fields {
		values[k] = v
	}
	msg := redis.XMessage{ID: "1234-0", Values: values}

	restored, err := parseStreamMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "1234-0", restored.MessageID)
	assert.Equal(t, original.JobID, restored.JobID)
	assert.Equal(t, original.EventType, restored.EventType)
	assert.Equal(t, original.UserID, restored.UserID)
	assert.Equal(t, original.Attempts, restored.Attempts)
	assert.Equal(t, "100.00", restored.Data["amount"])
}

func TestParseStreamMessage_MissingRequiredFields(t *testing.T) {
	msg := redis.XMessage{ID: "1234-0", Values: map[string]interface{}{
		"user_id": "user-1",
	}}
	_, err := parseStreamMessage(msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required fields")
}

func TestParseStreamMessage_InvalidDataJSON(t *testing.T) {
	msg := redis.XMessage{ID: "1234-0", Values: map[string]interface{}{
		"job_id":     "j1",
		"event_type": "test.event",
		"user_id":    "u1",
		"data":       "not-json{{{",
		"attempts":   "0",
		"created_at": "2025-01-01T00:00:00Z",
	}}
	_, err := parseStreamMessage(msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse data")
}

func TestParseStreamMessage_EmptyData(t *testing.T) {
	msg := redis.XMessage{ID: "1234-0", Values: map[string]interface{}{
		"job_id":     "j1",
		"event_type": "test.event",
		"user_id":    "u1",
		"data":       "",
		"attempts":   "0",
		"created_at": "2025-01-01T00:00:00Z",
	}}
	job, err := parseStreamMessage(msg)
	require.NoError(t, err)
	assert.NotNil(t, job.Data)
	assert.Empty(t, job.Data)
}

func TestStreamJob_WithLastError(t *testing.T) {
	job := StreamJob{
		JobID:     "j-err",
		EventType: "test.fail",
		UserID:    "u1",
		Data:      map[string]interface{}{},
		Attempts:  3,
		CreatedAt: "2025-01-01T00:00:00Z",
		LastError: "connection refused",
	}

	fields, err := job.toStreamFields()
	require.NoError(t, err)
	assert.Equal(t, "connection refused", fields["last_error"])

	msg := redis.XMessage{ID: "5678-0", Values: make(map[string]interface{})}
	for k, v := range fields {
		msg.Values[k] = v
	}

	restored, err := parseStreamMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "connection refused", restored.LastError)
}

func TestParseStreamMessage_ZeroAttempts(t *testing.T) {
	msg := redis.XMessage{ID: "1-0", Values: map[string]interface{}{
		"job_id":     "j1",
		"event_type": "test.event",
		"user_id":    "u1",
		"data":       `{"key":"val"}`,
		"attempts":   "0",
		"created_at": "2025-01-01T00:00:00Z",
	}}
	job, err := parseStreamMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, 0, job.Attempts)
}

func TestMaxAttemptsConstant(t *testing.T) {
	assert.Equal(t, 10, MaxAttempts())
}
