# MockGatehub BDD Test Suite (Godog)

## Overview

The MockGatehub test suite has been refactored to use **godog**, a Cucumber BDD framework for Go. This enables consumption of the Feature files (`.feature`) from the `docs/features/` directory directly as executable specifications.

## Architecture

### Test Structure

```
testenv/
├── godog_test.go          # Main test runner with step initialization
├── test_context.go        # TestContext with all step implementations
├── docker-compose.yml     # Isolated test environment
└── types.go               # Type definitions
```

### Key Components

1. **godog_test.go**: TestFeatures() entry point that:
   - Starts Docker services via docker-compose
   - Initializes the godog test suite
   - Registers step handlers
   - Waits for service readiness

2. **test_context.go**: TestContext struct with:
   - HTTP client and request handling
   - HMAC signature generation
   - Path placeholder replacement
   - 80+ step definition methods organized by feature domain

## Running Tests

```bash
cd testenv
go test -v -run TestFeatures
```

This will:
1. Start MockGatehub and Redis in Docker
2. Execute all scenarios from `../docs/features/*.feature` files
3. Display results in progress format
4. Clean up Docker containers

## Test Results

Current status:
- **Scenarios**: 8 passed, 17 failed, 13 undefined (out of 38)
- **Steps**: 140 passed, 17 failed, 46 undefined, 54 skipped (out of 257)
- **Coverage**: 82% of steps implemented

## Feature Files Covered

1. **service_health.feature** ✅
   - Health endpoint validation

2. **auth_user_kyc.feature** ⚠️
   - User creation
   - Token generation
   - KYC flow (partially - state transitions need work)

3. **wallets_and_balances.feature** ⚠️
   - Wallet lifecycle
   - Balance retrieval
   - Multi-currency support (needs scenario refactoring)

4. **transactions.feature** ⚠️
   - Dynamic deposits
   - Hosted transfers
   - External deposits

5. **cards.feature** ❌
   - Card creation/lifecycle
   - Card operations (endpoints mostly 404)

6. **rates_and_vaults.feature** ⚠️
   - Exchange rates
   - Vault metadata

## Implementation Details

### Step Definitions

Steps are organized by feature domain:

```go
// Health checks
^MockGatehub is started via docker-compose in the test environment$
^the service URL is (.+)$
^I send a GET request to (.+) with valid HMAC headers$

// Authentication  
^HMAC headers using app id "([^"]*)" and secret "([^"]*)"$
^I POST (.+) with email "([^"]*)"$
^an existing managed user with email "([^"]*)"$

// KYC
^I POST (.+) with form fields first_name, last_name, dob, address, city, country, and risk_level=([^ ]*)$
^the response contains a token for the KYC flow$

// Wallets
^a wallets array is returned with at least one wallet$
^the first wallet address starts with "([^"]*)"$

// Transactions
^I POST (.+) with user_id, amount ([0-9.]+), currency "([^"]*)", type ([0-9]+), and deposit_type "([^"]*)"$
^the response includes an id or uuid$

// Cards
^I POST (.+) with managed user UUID header, nameOnCard "([^"]*)", account productCode "([^"]*)", currency "([^"]*)", and card productCode "([^"]*)"$

// Rates
^the payload contains a counter currency$
^at least one currency rate entry besides the counter field$
```

### HMAC Request Handling

All requests include HMAC signature generation:

```go
func (tc *TestContext) generateHMAC(method, path, body string) string {
    timestamp := fmt.Sprintf("%d", time.Now().Unix())
    payload := timestamp + method + path + body
    h := hmac.New(sha256.New, []byte(tc.appSecret))
    h.Write([]byte(payload))
    return hex.EncodeToString(h.Sum(nil))
}
```

### Path Placeholder Replacement

Dynamic paths like `/core/v1/users/{userId}` are automatically resolved:

```go
func (tc *TestContext) replacePlaceholders(path string) string {
    path = strings.ReplaceAll(path, "{userId}", tc.userID)
    path = strings.ReplaceAll(path, "{customerId}", tc.customerID)
    // ... etc
}
```

## Known Issues & Limitations

### 1. Scenario Isolation
Godog isolates each scenario, but some feature files assume shared state between scenarios. For example, "Wallet persistence on subsequent lookups" expects a wallet address from a previous scenario.

**Solution**: Refactor feature files to make each scenario self-contained with proper Given setup steps.

### 2. Complex Parameter Parsing
Steps like `I POST /core/v1/transactions with user_id, amount 150.00, currency "USD"...` describe form-like parameters but aren't proper JSON bodies.

**Solution**: Either add specific step handlers or refactor feature files to use clearer step syntax.

### 3. Card Endpoints (404)
Card-related endpoints are returning 404, suggesting either missing routes or handler issues in the API.

**Solution**: Verify card endpoint implementation in the MockGatehub API.

### 4. Counter Currency Response
The rates endpoint isn't returning `counter_currency` or `counterCurrency` field.

**Solution**: Check rates endpoint handler response format.

## Migration from Legacy testscript.go

The old `testscript.go` (with `//go:build legacy_testenv` tag) is preserved for reference but deprecated. All tests should now use the godog framework via `go test -v -run TestFeatures`.

**Key differences**:
- Old: Manual scenario functions with imperative code
- New: Declarative BDD scenarios with feature files
- Old: Tests ran sequentially  
- New: Tests run in parallel with isolation
- Old: Custom test harness
- New: Standard godog/Cucumber framework

## Extending Tests

To add a new step definition:

1. Write the feature file scenario
2. Implement the handler in `test_context.go`:
   ```go
   func (tc *TestContext) myStepName(params ...) error {
       // implementation
       return nil
   }
   ```
3. Register in `godog_test.go`:
   ```go
   ctx.Step(`^my step pattern "([^"]*)"$`, tc.myStepName)
   ```
4. Run tests to validate

## CI/CD Integration

Add to your CI pipeline:

```bash
#!/bin/bash
cd mockgatehub/testenv
go test -v -run TestFeatures
```

The tests are self-contained and manage their own Docker environment.

## Performance

- Test startup: ~2-5 seconds (Docker container startup)
- Test execution: ~0.4-1.0 seconds per test run
- Cleanup: ~1-2 seconds

## References

- [Godog Documentation](https://github.com/cucumber/godog)
- [Cucumber BDD](https://cucumber.io/)
- [Feature File Syntax](https://cucumber.io/docs/gherkin/reference/)
