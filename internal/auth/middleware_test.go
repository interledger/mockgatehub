package auth

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReconstructURL(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		host    string
		tls     bool
		url     string
		want    string
	}{
		{
			name: "forwarded headers",
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "mockgatehub.interledger.test",
			},
			host: "localhost:25151",
			url:  "http://localhost/auth/v1/users/managed?clientId=abc",
			want: "https://mockgatehub.interledger.test/auth/v1/users/managed?clientId=abc",
		},
		{
			name:    "tls without forwarded headers",
			headers: map[string]string{},
			host:    "localhost:25151",
			tls:     true,
			url:     "http://localhost/id/v1/users/123",
			want:    "https://localhost:25151/id/v1/users/123",
		},
		{
			name:    "http fallback",
			headers: map[string]string{},
			host:    "localhost:25151",
			url:     "http://localhost/health",
			want:    "http://localhost:25151/health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", tt.url, nil)
			if err != nil {
				t.Fatalf("failed to build request: %v", err)
			}
			req.URL.Scheme = ""
			req.URL.Host = ""
			req.Host = tt.host
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			got := reconstructURL(req)
			assert.Equal(t, tt.want, got)
		})
	}
}
