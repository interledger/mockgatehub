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

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{
			name:    "exact match without wildcard",
			pattern: "/admin/fees",
			path:    "/admin/fees",
			want:    true,
		},
		{
			name:    "single wildcard matches user ID",
			pattern: "/admin/users/*/fees",
			path:    "/admin/users/00000000-0000-0000-0000-000000000001/fees",
			want:    true,
		},
		{
			name:    "wildcard does not match multiple segments",
			pattern: "/admin/*/fees",
			path:    "/admin/users/123/fees",
			want:    false,
		},
		{
			name:    "pattern with different path segments does not match",
			pattern: "/admin/users/*/fees",
			path:    "/admin/users/123/config",
			want:    false,
		},
		{
			name:    "pattern with different length does not match",
			pattern: "/admin/users/*/fees",
			path:    "/admin/users",
			want:    false,
		},
		{
			name:    "trailing slash is handled correctly",
			pattern: "/admin/users/*/fees",
			path:    "/admin/users/123/fees/",
			want:    true, // Trailing slash is trimmed, so it matches
		},
		{
			name:    "wildcard at different position",
			pattern: "/api/*/endpoint",
			path:    "/api/v1/endpoint",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.path)
			assert.Equal(t, tt.want, got, "matchPattern(%q, %q)", tt.pattern, tt.path)
		})
	}
}

func TestMatchesPublicPattern(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "user-specific fee GET endpoint",
			path: "/admin/users/00000000-0000-0000-0000-000000000001/fees",
			want: true,
		},
		{
			name: "user-specific fee PUT endpoint",
			path: "/admin/users/test-user-123/fees",
			want: true,
		},
		{
			name: "user-specific fee DELETE endpoint",
			path: "/admin/users/abc-def-ghi/fees",
			want: true,
		},
		{
			name: "non-public endpoint",
			path: "/core/v1/transactions",
			want: false,
		},
		{
			name: "global fees endpoint (not in patterns, but in PublicEndpoints)",
			path: "/admin/fees",
			want: false, // Should be handled by exact match in PublicEndpoints
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPublicPattern(tt.path)
			assert.Equal(t, tt.want, got, "matchesPublicPattern(%q)", tt.path)
		})
	}
}

