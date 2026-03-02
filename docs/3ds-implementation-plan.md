# 3DS Fixes — TDD Implementation Plan

Based on [gatehub-cards-explainer.md](gatehub-cards-explainer.md) sections 7–9. Ordered by impact: critical backend-breaking bugs first, then API compliance fixes.

---

## Progress

- ✅ **Step 1**: COMPLETED - Fix `GetPendingConfirmations` response wrapper
- ✅ **Step 2**: COMPLETED - Generalize `GetCardToken` route
- ✅ **Step 3**: COMPLETED - Fix `ConfirmThreeDS` response format
- ✅ **Step 4**: COMPLETED - Add POST alias for card limits
- ✅ **Step 5**: COMPLETED - Fix timeout format

---

## Step 1: Fix `GetPendingConfirmations` response wrapper ✅ DONE

**Why first:** This is the highest-impact bug. The wallet backend unmarshals into `PendingThreeDSConfirmationResponse{PendingConfirmations: [...]}` but MockGateHub returns a bare array. The backend silently gets `nil` — every `GetPendingThreeDSConfirmations()` call returns no results.

### 1a. Write/update unit test

**File:** `internal/handler/cards_test.go`

Update `TestGetPendingConfirmations_Empty` to assert the wrapped response shape:

```go
func TestGetPendingConfirmations_Empty(t *testing.T) {
    h, _ := setupCardsHandler(t)
    req := httptest.NewRequest(http.MethodGet, "/cards/v1/confirmations", nil)
    req.Header.Set("x-gatehub-managed-user-uuid", consts.TestUser1ID)
    w := httptest.NewRecorder()
    h.GetPendingConfirmations(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    var resp map[string]interface{}
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    confirmations, ok := resp["pendingConfirmations"].([]interface{})
    require.True(t, ok, "response must have pendingConfirmations key")
    assert.Empty(t, confirmations)
}
```

Add a new test with a seeded challenge to verify the wrapper works with data:

```go
func TestGetPendingConfirmations_WithChallenge(t *testing.T) {
    h, store := setupCardsHandler(t)

    challenge := &models.ThreeDSChallenge{
        TransactionID:    "3ds-pending-test",
        CardID:           "card-123",
        UserID:           consts.TestUser1ID,
        MerchantName:     "Test Shop",
        PurchaseAmount:   "50.00",
        PurchaseCurrency: "EUR",
        PurchaseDate:     time.Now().UTC().Format(time.RFC3339),
        Timeout:          time.Now().Add(5 * time.Minute),
        Status:           "pending",
        CreatedAt:        time.Now(),
    }
    require.NoError(t, store.CreateThreeDSChallenge(challenge))

    req := httptest.NewRequest(http.MethodGet, "/cards/v1/confirmations", nil)
    req.Header.Set("x-gatehub-managed-user-uuid", consts.TestUser1ID)
    w := httptest.NewRecorder()
    h.GetPendingConfirmations(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    var resp map[string]interface{}
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    confirmations, ok := resp["pendingConfirmations"].([]interface{})
    require.True(t, ok, "response must have pendingConfirmations key")
    assert.Len(t, confirmations, 1)
}
```

**Run tests — expect FAIL** (existing code returns bare array).

### 1b. Fix the handler

**File:** `internal/handler/cards.go`, `GetPendingConfirmations`

Change the two `sendJSON` calls:  
- Error path: `h.sendJSON(w, http.StatusOK, map[string]interface{}{"pendingConfirmations": []models.PendingThreeDSConfirmation{}})`
- Success path: `h.sendJSON(w, http.StatusOK, map[string]interface{}{"pendingConfirmations": pending})`

### 1c. Update E2E tests

**File:** `testenv/card_steps.go`

Update `responseIsArrayOfPending3DSConfirmations` to parse the wrapper:

```go
func (tc *TestContext) responseIsArrayOfPending3DSConfirmations(version int) error {
    var wrapper map[string]interface{}
    if err := json.Unmarshal(tc.lastResponseBody, &wrapper); err != nil {
        return fmt.Errorf("response is not an object: %w", err)
    }
    confirmations, ok := wrapper["pendingConfirmations"].([]interface{})
    if !ok {
        return fmt.Errorf("missing pendingConfirmations key in response: %s", string(tc.lastResponseBody))
    }
    if len(confirmations) == 0 {
        return fmt.Errorf("pendingConfirmations array is empty")
    }
    return nil
}
```

Update `eachConfirmationHasRequiredFields` similarly to unwrap from `pendingConfirmations`.

### 1d. Run all tests

```bash
make unit-tests   # should pass
make e2e-tests    # should pass
```

**Status: ✅ COMPLETE**
- Unit tests updated and passing
- Handler fixed to wrap response
- E2E steps updated to handle wrapper
- Files modified:
  - `internal/handler/cards.go` - wrapped response in `GetPendingConfirmations`
  - `internal/handler/cards_test.go` - updated and added tests
  - `testenv/card_steps.go` - updated E2E step definitions

---

## Step 2: Generalize `GetCardToken` route to handle all token types

**Why second:** The wallet backend calls `POST /cards/v1/token/{tokenType}` with types `pin`, `pin-change`, `card-data`, `apple-provisioning`, `google-provisioning`, `pai`. Only `card-data` works today — the rest 404.

### 2a. Write unit tests for new token types

**File:** `internal/handler/cards_test.go`

Add a table-driven test that covers multiple token types:

```go
func TestGetCardToken_AllTypes(t *testing.T) {
    tokenTypes := []string{"card-data", "pin", "pin-change", "apple-provisioning", "google-provisioning", "pai"}
    for _, tt := range tokenTypes {
        t.Run(tt, func(t *testing.T) {
            h, _ := setupCardsHandler(t)
            body := map[string]interface{}{"cardId": "card-123"}
            b, _ := json.Marshal(body)
            req := httptest.NewRequest(http.MethodPost, "/cards/v1/token/"+tt, bytes.NewReader(b))
            req.Header.Set("Content-Type", "application/json")
            req = cardChiParams(req, map[string]string{"tokenType": tt})

            w := httptest.NewRecorder()
            h.GetCardToken(w, req)

            assert.Equal(t, http.StatusOK, w.Code)
            var resp models.CardTokenResponse
            require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
            assert.Contains(t, resp.Token, "mock-"+tt+"-card-123")
            assert.Len(t, resp.Links, 1)
            assert.Contains(t, resp.Links[0].Href, tt)
        })
    }
}
```

**Run tests — expect FAIL** (handler hardcodes `card-data`).

### 2b. Fix the route and handler

**File:** `cmd/mockgatehub/main.go`

Change route:
```go
// Before
r.Post("/token/card-data", h.GetCardToken)
// After
r.Post("/token/{tokenType}", h.GetCardToken)
```

**File:** `internal/handler/cards.go`, `GetCardToken`

```go
func (h *Handler) GetCardToken(w http.ResponseWriter, r *http.Request) {
    tokenType := chi.URLParam(r, "tokenType")
    if tokenType == "" {
        tokenType = "card-data"
    }
    logger.Info("get card token called", zap.String("token_type", tokenType))

    var req models.GetCardTokenArgs
    if err := h.decodeJSON(r, &req); err != nil {
        h.sendError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    tokenValue := fmt.Sprintf("mock-%s-%s", tokenType, req.CardID)
    response := models.CardTokenResponse{
        Token: tokenValue,
        Links: []models.CardTokenLink{
            {
                Href:   fmt.Sprintf("/cards/v1/proxy/clientDevice/%s?token=%s", tokenType, tokenValue),
                Rel:    "data",
                Method: "GET",
            },
        },
    }

    h.sendJSON(w, http.StatusOK, response)
}
```

### 2c. Update existing unit test

Update `TestGetCardToken_Success` to match the new token format (if the existing test still hardcodes `mock-card-data-`). The token should now come from the URL param, so ensure the chi context is set.

### 2d. Update E2E feature + steps

**File:** `features/cards.feature`

Update the card-data scenario and optionally add a pin token type test:

```gherkin
  Scenario: Get card token for secure data access
    Given a managed customer with a card exists
    When I POST /cards/v1/token/card-data with cardId and managed user UUID header
    Then the response status is 200
    And the response contains a token starting with "mock-card-data-"
    And the response contains a links array with at least one entry

  Scenario: Get card token for PIN change
    Given a managed customer with a card exists
    When I POST /cards/v1/token/pin-change with cardId and managed user UUID header
    Then the response status is 200
    And the response contains a token starting with "mock-pin-change-"
    And the response contains a links array with at least one entry
```

The step definition `postCardTokenWithManagedUser` already sends to whatever path is given, so no step code changes needed.

### 2e. Run all tests

```bash
make unit-tests
make e2e-tests
```

**Status: ✅ COMPLETE**
- Unit tests added for all 6 token types and passing
- Route changed from `/token/card-data` to `/token/{tokenType}`
- Handler updated to extract and use tokenType parameter
- Backward compatible - existing card-data calls still work
- Files modified:
  - `cmd/mockgatehub/main.go` - parameterized route
  - `internal/handler/cards.go` - extract and use tokenType
  - `internal/handler/cards_test.go` - added table-driven test

---

## Step 3: Fix `ConfirmThreeDS` response format

**Why third:** API compliance fix. The wallet backend only checks HTTP status code, so this doesn't break functionality, but violates the GateHub API contract. Small change, easy win.

### 3a. Write/update unit tests

**File:** `internal/handler/cards_test.go`

Update `TestConfirmThreeDS_Approve` to assert `confirmed: true` instead of `status: "approved"`:

```go
func TestConfirmThreeDS_Approve(t *testing.T) {
    h, store := setupCardsHandler(t)
    challenge := &models.ThreeDSChallenge{
        TransactionID: "3ds-test",
        CardID:        "card-123",
        UserID:        consts.TestUser1ID,
        Status:        "pending",
        Timeout:       time.Now().Add(5 * time.Minute),
    }
    require.NoError(t, store.CreateThreeDSChallenge(challenge))

    body := map[string]interface{}{"confirmed": true, "authMethod": "biometric"}
    b, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost, "/cards/v1/transaction/3ds-test", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req = cardChiParams(req, map[string]string{"txID": "3ds-test"})

    w := httptest.NewRecorder()
    h.ConfirmThreeDS(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    var resp map[string]interface{}
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    assert.Equal(t, "3ds-test", resp["transactionId"])
    assert.Equal(t, true, resp["confirmed"])
}
```

Update `TestConfirmThreeDS_Decline` similarly to assert `confirmed: false`.

**Run tests — expect FAIL** (handler returns `status` field).

### 3b. Fix the handler

**File:** `internal/handler/cards.go`, `ConfirmThreeDS`

Change the response:
```go
h.sendJSON(w, http.StatusOK, map[string]interface{}{
    "transactionId": txID,
    "confirmed":     req.Confirmed,
})
```

### 3c. Update E2E feature + steps

**File:** `features/cards.feature`

```gherkin
  Scenario: Confirm 3DS payment (approve)
    Given a managed customer with a pending 3DS challenge exists
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed true, authMethod "pin"
    Then the response status is 200
    And the response contains "confirmed" with value true

  Scenario: Decline 3DS payment
    Given a managed customer with a pending 3DS challenge exists
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed false, authMethod "pin"
    Then the response status is 200
    And the response contains "confirmed" with value false
```

**File:** `testenv/card_steps.go`

Replace `responseIndicatesSuccess` / `responseIndicatesDeclined` with a step that checks the `confirmed` field:

```go
func (tc *TestContext) responseContainsConfirmedValue(expected string) error {
    var result map[string]interface{}
    if err := json.Unmarshal(tc.lastResponseBody, &result); err != nil {
        return err
    }
    confirmed, ok := result["confirmed"]
    if !ok {
        return fmt.Errorf("missing 'confirmed' field in response")
    }
    expectedBool := expected == "true"
    if confirmed != expectedBool {
        return fmt.Errorf("expected confirmed=%v, got %v", expectedBool, confirmed)
    }
    return nil
}
```

### 3d. Run all tests

```bash
make unit-tests
make e2e-tests
```

**Status: ✅ COMPLETE**
- Unit tests updated to expect `confirmed` field instead of `status`
- Handler response changed to return `{"transactionId": "...", "confirmed": true/false}`
- Feature file updated with new response assertions
- E2E step definitions and godog registrations updated
- Files modified:
  - `internal/handler/cards.go` - response format fix
  - `internal/handler/cards_test.go` - updated assertions
  - `features/cards.feature` - new assertion syntax
  - `testenv/godog_test.go` - updated step registrations
  - `testenv/card_steps.go` - new step definition

---

## Step 4: Add POST alias for card limits

**Why fourth:** GateHub official API uses `POST` for create/override limits. MockGateHub only has `PUT`. The wallet backend currently uses PUT, but adding POST ensures forward compatibility and API compliance.

### 4a. Write unit test

**File:** `internal/handler/cards_test.go`

```go
func TestUpdateCardLimits_POST(t *testing.T) {
    h, store := setupCardsHandler(t)
    _, _, cardID := seedCard(t, store)

    limits := []models.CardLimit{{Type: "dailyOverall", Limit: 3000, Currency: "EUR"}}
    b, _ := json.Marshal(limits)
    req := httptest.NewRequest(http.MethodPost, "/cards/v1/cards/"+cardID+"/limits", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req = cardChiParams(req, map[string]string{"cardID": cardID})

    w := httptest.NewRecorder()
    h.UpdateCardLimits(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
}
```

This test calls the same handler via POST; it should already pass since the handler doesn't check HTTP method. The real fix is the route.

### 4b. Add the route

**File:** `cmd/mockgatehub/main.go`

```go
r.Get("/cards/{cardID}/limits", h.GetCardLimits)
r.Put("/cards/{cardID}/limits", h.UpdateCardLimits)
r.Post("/cards/{cardID}/limits", h.UpdateCardLimits)  // GateHub uses POST
```

### 4c. Run all tests

```bash
make unit-tests
make e2e-tests
```

**Status: ✅ COMPLETE**
- Unit test added for POST endpoint
- Route registered with `r.Post("/cards/{cardID}/limits", h.UpdateCardLimits)`
- Both PUT and POST now route to the same handler
- Files modified:
  - `cmd/mockgatehub/main.go` - added POST route
  - `internal/handler/cards_test.go` - added POST test

---

## Step 5: Fix `timeout` field format (seconds-as-string)

**Why fifth:** API compliance. GateHub returns `"300"` (seconds remaining as string). MockGateHub returns RFC3339 timestamp. The wallet backend passes this through as an opaque string today, but format consistency matters for correctness.

### 5a. Write unit test

**File:** `internal/handler/cards_test.go`

Add a test that verifies timeout is seconds-as-string:

```go
func TestGetPendingConfirmations_TimeoutFormat(t *testing.T) {
    h, store := setupCardsHandler(t)

    challenge := &models.ThreeDSChallenge{
        TransactionID:    "3ds-timeout-test",
        CardID:           "card-123",
        UserID:           consts.TestUser1ID,
        MerchantName:     "Shop",
        PurchaseAmount:   "10.00",
        PurchaseCurrency: "EUR",
        PurchaseDate:     time.Now().UTC().Format(time.RFC3339),
        Timeout:          time.Now().Add(5 * time.Minute),
        Status:           "pending",
        CreatedAt:        time.Now(),
    }
    require.NoError(t, store.CreateThreeDSChallenge(challenge))

    req := httptest.NewRequest(http.MethodGet, "/cards/v1/confirmations", nil)
    req.Header.Set("x-gatehub-managed-user-uuid", consts.TestUser1ID)
    w := httptest.NewRecorder()
    h.GetPendingConfirmations(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    var resp map[string]json.RawMessage
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

    var confirmations []map[string]interface{}
    require.NoError(t, json.Unmarshal(resp["pendingConfirmations"], &confirmations))
    require.Len(t, confirmations, 1)

    timeout := confirmations[0]["timeout"].(string)
    // Should be a numeric string (seconds remaining), not RFC3339
    _, err := strconv.Atoi(timeout)
    assert.NoError(t, err, "timeout should be seconds-as-string, got: %s", timeout)
}
```

**Run tests — expect FAIL** (returns RFC3339).

### 5b. Fix the handler

**File:** `internal/handler/cards.go`, `GetPendingConfirmations`

Change the timeout mapping inside the loop:

```go
remaining := int(time.Until(c.Timeout).Seconds())
if remaining < 0 {
    remaining = 0
}

pending = append(pending, models.PendingThreeDSConfirmation{
    TransactionID:    c.TransactionID,
    MerchantName:     c.MerchantName,
    PurchaseAmount:   c.PurchaseAmount,
    PurchaseCurrency: c.PurchaseCurrency,
    PurchaseDate:     c.PurchaseDate,
    Timeout:          fmt.Sprintf("%d", remaining),
})
```

### 5c. Run all tests

```bash
make unit-tests
make e2e-tests
```

**Status: ✅ COMPLETE**
- Unit test added to verify timeout is seconds-as-string
- Handler updated to calculate `time.Until(timeout).Seconds()` instead of `timeout.Format(time.RFC3339)`
- Response now returns `"300"` (seconds remaining) instead of `"2026-03-01T13:47:06+02:00"`
- Files modified:
  - `internal/handler/cards.go` - timeout format fix
  - `internal/handler/cards_test.go` - added timeout format test

---

## Summary

✅ **ALL STEPS COMPLETED**

| Step | Fix | Impact | Actual Effort |
|------|-----|--------|---------------|
| 1 | Wrap `GetPendingConfirmations` response in `{"pendingConfirmations": [...]}` | **Critical** — backend gets nil results | ✅ Complete |
| 2 | Generalize `/token/card-data` → `/token/{tokenType}` | **Critical** — PIN and provisioning 404 | ✅ Complete |
| 3 | `ConfirmThreeDS` response: `confirmed` bool instead of `status` string | Compliance | ✅ Complete |
| 4 | Add `POST` alias for card limits | Compliance | ✅ Complete |
| 5 | Timeout field: seconds-as-string instead of RFC3339 | Compliance | ✅ Complete |

### Files Modified

**Handler Logic** (`internal/handler/cards.go`):
- `GetPendingConfirmations`: Wrapped response + fixed timeout format
- `GetCardToken`: Extract tokenType from URL param
- `ConfirmThreeDS`: Return `confirmed` field instead of `status`

**Routes** (`cmd/mockgatehub/main.go`):
- `/token/{tokenType}` (parameterized, supports all 6 token types)
- `POST /cards/{cardID}/limits` (added POST alias)

**Tests** (`internal/handler/cards_test.go`):
- `TestGetPendingConfirmations_Empty`: Updated wrapper assertion
- `TestGetPendingConfirmations_WithChallenge`: New test with data
- `TestGetPendingConfirmations_TimeoutFormat`: Seconds-as-string verification
- `TestGetCardToken_AllTokenTypes`: Table-driven test for 6 token types
- `TestConfirmThreeDS_Approve`: Updated to check `confirmed: true`
- `TestConfirmThreeDS_Decline`: Updated to check `confirmed: false`
- `TestUpdateCardLimits_POST`: New test for POST route

**E2E Tests** (`testenv/`):
- `godog_test.go`: Updated step registrations
- `card_steps.go`: Updated response assertions and helper functions
- `features/cards.feature`: Updated scenario assertions

### Testing Results

All unit tests passing ✅
Ready for E2E testing ✅

---

## Summary (Original)

**Total: ~55 minutes** following strict TDD cycle per step.

### Per-step TDD cycle

```
1. Write/update failing unit test(s)
2. Run `make unit-tests` — confirm test fails for the right reason
3. Implement the fix (smallest change possible)
4. Run `make unit-tests` — confirm test passes
5. Update E2E test expectations if affected
6. Run `make e2e-tests` — confirm E2E passes
7. Commit with conventional message (e.g., `fix: wrap GetPendingConfirmations response`)
```

### Commit messages

```
fix: wrap GetPendingConfirmations response in pendingConfirmations object
fix: generalize GetCardToken route to accept all token types
fix: align ConfirmThreeDS response with GateHub API (confirmed bool)
fix: add POST alias for card limits endpoint
fix: return timeout as seconds-as-string in pending confirmations
```
