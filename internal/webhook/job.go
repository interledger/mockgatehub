package webhook

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// StreamJob represents a webhook job read from a Redis Stream.
// MessageID is the Redis stream entry ID used for XACK.
type StreamJob struct {
	MessageID string
	JobID     string
	EventType string
	UserID    string
	Data      map[string]interface{}
	Attempts  int
	CreatedAt string
	LastError string
}

// toStreamFields converts a StreamJob into a map suitable for XADD.
func (j *StreamJob) toStreamFields() (map[string]interface{}, error) {
	dataJSON, err := json.Marshal(j.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize job data: %w", err)
	}

	fields := map[string]interface{}{
		"job_id":     j.JobID,
		"event_type": j.EventType,
		"user_id":    j.UserID,
		"data":       string(dataJSON),
		"attempts":   strconv.Itoa(j.Attempts),
		"created_at": j.CreatedAt,
	}
	if j.LastError != "" {
		fields["last_error"] = j.LastError
	}
	return fields, nil
}

// parseStreamMessage converts a Redis stream message into a StreamJob.
func parseStreamMessage(msg redis.XMessage) (StreamJob, error) {
	jobID, _ := msg.Values["job_id"].(string)
	eventType, _ := msg.Values["event_type"].(string)
	userID, _ := msg.Values["user_id"].(string)
	createdAt, _ := msg.Values["created_at"].(string)
	lastError, _ := msg.Values["last_error"].(string)

	if jobID == "" || eventType == "" {
		return StreamJob{}, fmt.Errorf("stream message %s missing required fields", msg.ID)
	}

	var data map[string]interface{}
	if dataStr, ok := msg.Values["data"].(string); ok && dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return StreamJob{}, fmt.Errorf("failed to parse data field in %s: %w", msg.ID, err)
		}
	}
	if data == nil {
		data = map[string]interface{}{}
	}

	attempts := 0
	if attStr, ok := msg.Values["attempts"].(string); ok {
		attempts, _ = strconv.Atoi(attStr)
	}

	return StreamJob{
		MessageID: msg.ID,
		JobID:     jobID,
		EventType: eventType,
		UserID:    userID,
		Data:      data,
		Attempts:  attempts,
		CreatedAt: createdAt,
		LastError: lastError,
	}, nil
}
