# GitHub Copilot Instructions for MockGatehub

## Project Context

**Purpose**: MockGatehub is a lightweight Go mock implementation of the Gatehub API for local development and testing of wallet applications that integrate with Gatehub.

**Why it exists**: Removes dependency on real Gatehub credentials and services, enabling fully local development without external dependencies, predictable behavior for CI/CD, and rapid iteration.

**Tech Stack**:
- Language: Go 1.24+
- HTTP Router: chi v5 (lightweight, idiomatic)
- Storage: Dual backend (memory for tests, Redis for runtime)
- Containerization: Docker multi-stage build
- Testing: testify for assertions

## Repository Structure

```
mockgatehub/
├── cmd/mockgatehub/           # Application entry point
│   └── main.go                # HTTP server setup, routing
├── internal/                  # Private application code
│   ├── auth/                  # HMAC signature generation/validation
│   ├── models/                # Domain & API models
│   ├── storage/               # Storage layer (memory/redis)
│   ├── handler/               # HTTP handlers (auth, identity, core, rates, cards)
│   ├── webhook/               # Webhook delivery system
│   ├── consts/                # Constants (currencies, vault IDs, rates)
│   ├── utils/                 # Utilities
│   └── logger/                # Logging setup
├── testenv/                   # Isolated integration test environment
│   ├── docker-compose.yml    # Test-only compose (ports 28080, 26380)
│   ├── testscript.go         # Go-based integration test suite
│   └── README.md             # Test environment documentation
├── web/                       # Static web assets
│   └── kyc-iframe.html       # KYC iframe HTML
├── Dockerfile                 # Multi-stage Docker build
├── .releaserc.json           # Semantic-release configuration
├── go.mod                     # Go module definition
├── README.md                  # User documentation
└── AGENTS.md                  # Comprehensive development guide
```

## Critical Constraints

1. **API Compliance**: MockGatehub must be a drop-in Gatehub replacement. Applications expect exact API compliance.
2. **Sandbox Parity Only**: Focus on happy paths and sandbox environment behavior. Production features out of scope.
3. **Multi-Currency Required**: Support all 11 currencies (XRP, USD, EUR, GBP, ZAR, MXN, SGD, CAD, EGG, PEB, PKR).
4. **Immutable Vault UUIDs**: Vault IDs are hardcoded and must never change (applications may store these).
5. **testenv/ Maintenance**: The `testenv/` directory is NOT optional. When making changes:
   - Add test cases to `testenv/testscript.go` for new endpoints
   - Update assertions if API responses change
   - Ensure backward compatibility (applications depend on exact Gatehub response format)
   - Run integration tests: `cd testenv && go run testscript.go`

## Key Architecture

### Storage Layer
- Interface-based design: enables swapping memory/Redis without code changes
- Memory implementation: uses `sync.RWMutex` for thread safety
- Redis implementation: JSON serialization, atomic operations for balances
- Seeder: pre-creates test users with balances for testing

### Authentication (HMAC Signatures)
- Format: `HMAC-SHA256(timestamp + method + path + body, secret)`
- Request headers: `x-gatehub-app-id`, `x-gatehub-timestamp`, `x-gatehub-signature`
- Used for both incoming request validation and outgoing webhooks

### Multi-Currency System
- All 11 currencies must be supported in responses
- Vault UUIDs hardcoded in `internal/consts/consts.go` (never change existing ones)
- Balance endpoint returns all currencies even if 0.00
- Exchange rates hardcoded vs USD

### KYC Flow
1. `POST /id/v1/users/{userID}/hubs/{gatewayID}` – Initiates KYC
2. `GET /?paymentType=onboarding&bearer={token}` – Serves KYC iframe HTML
3. `POST /iframe/submit` – Iframe form submission, updates user to accepted
4. Webhook `id.verification.accepted` emitted asynchronously

### Webhook System
- Manager (`internal/webhook/manager.go`) handles async delivery
- Event types: `id.verification.accepted`, `core.deposit.completed`
- 3 retry attempts with exponential backoff (1s, 2s, 4s)
- Signs requests with HMAC

## Development Workflow

### Running Locally

```bash
# In-memory mode (quick testing)
go run ./cmd/mockgatehub

# With Redis (production-like)
MOCKGATEHUB_REDIS_URL=redis://localhost:6379 \
MOCKGATEHUB_REDIS_DB=1 \
go run ./cmd/mockgatehub
```

### Testing

```bash
# Unit tests
go test -v -race -coverprofile=coverage.out ./...

# Integration tests (isolated environment)
cd testenv
go run testscript.go

# Local validation before pushing
go mod tidy
go test ./...
cd testenv && go run testscript.go
cd .. && go build ./cmd/mockgatehub
```

### Docker Build & Test

```bash
# Build fresh image
docker build -t local-mockgatehub .

# Test in isolated environment
cd testenv
go run testscript.go

# Full stack with docker-compose
docker compose up -d mockgatehub
docker compose logs -f mockgatehub
```

## CI/CD Pipeline

### GitHub Actions Workflows

**pr-validation.yml**: Runs on pull requests
- PR title validation (Conventional Commits)
- Unit and integration tests
- Docker build (no push) with GHA cache
- Coverage upload to Codecov

**release.yml**: Runs on push to main
- All tests
- Semantic-release to determine version and create tag
- Outputs `new_release_published` to conditionally trigger Docker job
- Docker build & push to ghcr.io (multi-platform: amd64)

### Semantic Versioning

- `feat`: minor release
- `fix`, `perf`, `docs`, `refactor`, `build`: patch release
- `ci`, `test`, `chore`: no release
- First release: v1.0.0 (from initial feat commit)
- Tag format: `v${version}`
- Config: `.releaserc.json`

### Docker Configuration

- Multi-stage build: golang:1.24-alpine → alpine:latest
- Platform: linux/amd64 (arm64 support can be added later)
- GitHub Actions cache: `cache-from: type=gha` and `cache-to: type=gha,mode=max`
- Non-root user: mockgatehub (UID/GID 1000)
- Health check on /health endpoint
- Published to: ghcr.io/interledger/mockgatehub

## Common Patterns

### Error Handling

```go
if req.UserID == "" {
    h.sendError(w, http.StatusBadRequest, "user_id is required")
    return
}
```

### JSON Response Helpers

```go
func (h *Handler) sendJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}
```

### Async Operations

```go
go func() {
    if err := h.webhookManager.Send(event); err != nil {
        logger.Error.Printf("Webhook delivery failed: %v", err)
    }
}()
```

## When Adding New Endpoints

1. **Define Models**: Add request/response DTOs to `internal/models/api.go`
2. **Implement Handler**: Add method to appropriate `internal/handler/{domain}.go`
3. **Add Route**: Register in `cmd/mockgatehub/main.go` setupRoutes
4. **Write Tests**: Create table-driven test in `{domain}_test.go` and integration test in testenv
5. **Update Docs**: Add endpoint to README.md API section

## When Modifying Storage

1. Update `internal/storage/interface.go`
2. Update **both** memory.go AND redis.go implementations
3. Add tests covering new functionality
4. Update seeder if affecting test user creation

## When Changing Constants

1. Update `internal/consts/consts.go`
2. **NEVER** change existing vault UUIDs (immutable)
3. Update all test files with hardcoded values
4. Update README.md tables

## Testing Checklist

- [ ] Unit tests pass: `go test ./...`
- [ ] Coverage acceptable: ≥80%
- [ ] Integration test passes: `cd testenv && go run testscript.go`
- [ ] Docker build succeeds: `docker build -t local-mockgatehub .`
- [ ] Full stack starts: `docker compose up`
- [ ] Application works with MockGatehub

## Key Files Reference

**Must Review Before Coding**:
1. `internal/consts/consts.go` - All constants
2. `internal/storage/interface.go` - Storage contract
3. `internal/models/models.go` - Domain models
4. `cmd/mockgatehub/main.go` - Routing configuration

**Frequently Modified**:
1. `internal/handler/*.go` - API endpoint implementations
2. `internal/storage/memory.go` - In-memory storage logic
3. `internal/webhook/manager.go` - Webhook delivery

**Rarely Touch**:
1. `internal/logger/logger.go` - Basic logging setup
2. `internal/utils/utils.go` - Utility functions
3. `Dockerfile` - Container build configuration

## Environment Variables

```bash
MOCKGATEHUB_PORT=8080                          # HTTP port
MOCKGATEHUB_REDIS_URL=redis://localhost:6379  # Redis connection
MOCKGATEHUB_REDIS_DB=1                         # Redis database number
WEBHOOK_URL=http://your-app:3003/gatehub-webhooks
WEBHOOK_SECRET=your-secret-here
```

## Logging Guidelines

**Important**: It's safe to log sensitive values (app IDs, bearer tokens, amounts, etc.) in MockGatehub because:
- This is a development/testing mock service, not a production system
- Applications running against it are also in local test environments
- Verbose logging helps with debugging integrations and identifying issues
- No real credentials or production data flows through this service

**Logging Standards**:
- Use zap structured logging via `logger.Info()`, `logger.Warn()`, `logger.Error()`, `logger.Debug()`
- Include contextual fields: `zap.String("key", value)`, `zap.Error(err)`, `zap.Int("value", num)`, etc.
- Log all significant operations: user creation, wallet operations, deposits, KYC state changes
- Include identifiers (user IDs, wallet addresses, transaction IDs) for traceability
- Use human-readable log levels:
  - `Info`: Normal operations (user created, deposit received)
  - `Warn`: Non-fatal issues (invalid input, fallback behavior)
  - `Error`: Operations that failed (database error, webhook failed)
  - `Debug`: Detailed diagnostic info (token resolution, balance calculations)

**Example**:
```go
logger.Info("deposit created successfully", 
    zap.String("user_id", userID),
    zap.String("amount", amountStr),
    zap.String("currency", currency),
    zap.String("wallet_address", address),
)
```

## Critical Notes for AI Agents

1. **ALWAYS run testenv tests after changes**: `cd testenv && go run testscript.go`
2. **Maintain API compatibility**: Applications rely on exact Gatehub response format
3. **Never modify generated/immutable content**: Vault UUIDs, currency codes
4. **Test both storage backends**: Changes must work with memory AND Redis
5. **Update AGENTS.md if you discover outdated guidance**: Keep instructions fresh
6. **Push to branch first**: Create PR to test against main branch protection
7. **Verify release workflow runs**: Check that semantic-release creates tags and Docker pushes
8. **Use zap structured logging**: Always include context fields for debugging

## Success Metrics

Your changes should maintain or improve:
- **Test Coverage**: ≥80%
- **API Compliance**: Applications run without modification
- **Response Time**: All endpoints < 100ms (local)
- **Memory Usage**: < 100MB for in-memory mode
- **Build Time**: Docker builds under 2 minutes

## Troubleshooting

**Tests failing with Redis**: Ensure Redis running and DB is empty (`redis-cli -n 1 FLUSHDB`)

**Docker build fails**: Check Dockerfile paths, verify `go.mod` and `go.sum` present, ensure no syntax errors (`go build ./...`)

**Webhooks not arriving**: Check `WEBHOOK_URL`, verify application backend running, check Docker logs

**Release workflow skips Docker job**: Verify semantic-release sets `new_release_published` output correctly

---

**Reference**: See [AGENTS.md](AGENTS.md) for comprehensive development guide with detailed architecture patterns, examples, and troubleshooting.
