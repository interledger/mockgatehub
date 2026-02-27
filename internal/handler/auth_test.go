package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mockgatehub/internal/models"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOrgTestHandler(t *testing.T, orgID string, setupOrg bool) (*Handler, *storage.MemoryStorage) {
	t.Helper()
	store := storage.NewMemoryStorage()
	wm := webhook.NewManager("", "test-secret", nil, nil, "")
	h := NewHandler(store, wm)

	if setupOrg {
		org := &models.Organization{
			ID:         orgID,
			APIBaseURL: "https://api.gatehub.net",
			TwoFAType:  "sms",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, store.CreateOrganization(org))
	}

	return h, store
}

func TestUpdateOrganizationConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		orgID          string
		requestBody    interface{}
		setupOrg       bool
		expectedStatus int
		checkResponse  func(t *testing.T, body map[string]interface{})
	}{
		{
			name:  "successful update with totp",
			orgID: "test-org",
			requestBody: map[string]string{
				"apiBaseUrl": "https://api.example.com",
				"type2fa":    "totp",
			},
			setupOrg:       true,
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body map[string]interface{}) {
				assert.Equal(t, "test-org", body["id"])
				assert.Equal(t, "https://api.example.com", body["apiBaseUrl"])
				assert.Equal(t, "totp", body["type2fa"])
				assert.NotEmpty(t, body["createdAt"])
				assert.NotEmpty(t, body["updatedAt"])
			},
		},
		{
			name:  "successful update with sms",
			orgID: "test-org",
			requestBody: map[string]string{
				"apiBaseUrl": "https://api.secure.com",
				"type2fa":    "sms",
			},
			setupOrg:       true,
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body map[string]interface{}) {
				assert.Equal(t, "https://api.secure.com", body["apiBaseUrl"])
				assert.Equal(t, "sms", body["type2fa"])
			},
		},
		{
			name:  "organization not found",
			orgID: "nonexistent",
			requestBody: map[string]string{
				"apiBaseUrl": "https://api.example.com",
				"type2fa":    "sms",
			},
			setupOrg:       false,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:  "invalid URL - not HTTPS",
			orgID: "test-org",
			requestBody: map[string]string{
				"apiBaseUrl": "http://api.example.com",
				"type2fa":    "sms",
			},
			setupOrg:       true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:  "invalid URL - empty",
			orgID: "test-org",
			requestBody: map[string]string{
				"apiBaseUrl": "",
				"type2fa":    "sms",
			},
			setupOrg:       true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:  "invalid 2FA type",
			orgID: "test-org",
			requestBody: map[string]string{
				"apiBaseUrl": "https://api.example.com",
				"type2fa":    "invalid",
			},
			setupOrg:       true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:  "empty 2FA type",
			orgID: "test-org",
			requestBody: map[string]string{
				"apiBaseUrl": "https://api.example.com",
				"type2fa":    "",
			},
			setupOrg:       true,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := setupOrgTestHandler(t, tt.orgID, tt.setupOrg)

			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPatch, "/auth/v1/users/organization/"+tt.orgID, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("organizationID", tt.orgID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			h.UpdateOrganizationConfiguration(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkResponse != nil && w.Code == http.StatusOK {
				var result map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &result)
				require.NoError(t, err)
				tt.checkResponse(t, result)
			}
		})
	}
}

func TestUpdateOrganizationConfiguration_PersistsInStorage(t *testing.T) {
	h, store := setupOrgTestHandler(t, "test-org", true)

	body, _ := json.Marshal(map[string]string{
		"apiBaseUrl": "https://api.persistent.com",
		"type2fa":    "totp",
	})

	req := httptest.NewRequest(http.MethodPatch, "/auth/v1/users/organization/test-org", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("organizationID", "test-org")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateOrganizationConfiguration(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify the organization was updated in storage
	org, err := store.GetOrganization("test-org")
	require.NoError(t, err)
	assert.Equal(t, "https://api.persistent.com", org.APIBaseURL)
	assert.Equal(t, "totp", org.TwoFAType)
}

func TestUpdateOrganizationConfiguration_TimestampFormat(t *testing.T) {
	h, _ := setupOrgTestHandler(t, "test-org", true)

	body, _ := json.Marshal(map[string]string{
		"apiBaseUrl": "https://api.example.com",
		"type2fa":    "sms",
	})

	req := httptest.NewRequest(http.MethodPatch, "/auth/v1/users/organization/test-org", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("organizationID", "test-org")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.UpdateOrganizationConfiguration(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)

	// Verify timestamp format is "YYYY-MM-DD HH:MM:SS.ssssss" (space-separated, not ISO 8601)
	createdAt, ok := result["createdAt"].(string)
	require.True(t, ok)
	_, err = time.Parse("2006-01-02 15:04:05.999999", createdAt)
	assert.NoError(t, err, "createdAt should be in GateHub timestamp format")

	updatedAt, ok := result["updatedAt"].(string)
	require.True(t, ok)
	_, err = time.Parse("2006-01-02 15:04:05.999999", updatedAt)
	assert.NoError(t, err, "updatedAt should be in GateHub timestamp format")
}
