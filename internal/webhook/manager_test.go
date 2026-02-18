package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewManager(t *testing.T) {
	manager := NewManager("http://example.com/webhook", "secret", nil)

	assert.NotNil(t, manager)
	assert.Equal(t, "http://example.com/webhook", manager.webhookURL)
	assert.Equal(t, "secret", manager.webhookSecret)
	assert.NotNil(t, manager.httpClient)
}

func TestSendAsync_NoURL(t *testing.T) {
	manager := NewManager("", "secret", nil)

	// Should not panic when queue is nil and URL is empty
	manager.SendAsync("test.event", "user-123", map[string]interface{}{}, 0)
}
