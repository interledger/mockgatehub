# GitHub Copilot Instructions for MockGatehub

## Project Context

**Purpose**: MockGatehub is a Go mock implementation of the GateHub API for local development and testing of wallet applications that integrate with GateHub.

**Why it exists**: Removes dependency on real GateHub credentials and services, enabling fully local development, predictable behavior for CI/CD, and rapid iteration.

**Tech Stack**:
- Language: Go 1.24+
- HTTP Router: chi v5
- Storage: Dual backend (memory for tests, Redis for runtime)
- Webhook Queue: Redis-backed sorted-set job queue with background worker
- Logging: Zap structured logging
- Testing: testify (unit), godog/Cucumber (BDD E2E)
- Containerization: Docker multi-stage build (linux/amd64 + linux/arm64)
- CI/CD: GitHub Actions with semantic-release

## Repository Structure

```
mockgatehub/
├── cmd/mockgatehub/           # Entry point: server setup, routing
│   ├── main.go
│   └── main_test.go
├── internal/
│   ├── auth/                  # HMAC signature validation + HTTP middleware
│   │   ├── middleware.go      # Global auth middleware with public endpoint whitelist
│   │   └── signature.go      # Signature generation and verification
│   ├── config/                # Environment variable configuration
│   │   └── config.go
│   ├── consts/                # Constants (currencies, vault IDs, rates, statuses)
│   │   └── consts.go
│   ├── handler/               # HTTP handlers
│   │   ├── handler.go         # Handler struct, health check, iframe handlers
│   │   ├── helpers.go         # sendJSON, sendError, setCORSHeaders, decodeJSON
│   │   ├── auth.go            # /auth/v1 endpoints
│   │   ├── identity.go        # /id/v1 endpoints (KYC)
│   │   ├── core.go            # /core/v1 endpoints (wallets, transactions)
│   │   ├── cards.go           # /cards/v1 endpoints (full card lifecycle)
│   │   ├── fees.go            # Fee configuration and admin API
│   │   └── rates.go           # /rates/v1 endpoints
│   ├── logger/                # Zap logger setup
│   ├── models/                # Domain & API models
│   │   ├── models.go          # User, Wallet, Transaction
│   │   ├── api.go             # Request/response DTOs
│   │   ├── messages.go        # Complex response types
│   │   └── cards.go           # Card-related models
│   ├── storage/               # Storage layer
│   │   ├── interface.go       # Storage contract (~30 methods)
│   │   ├── memory.go          # In-memory implementation
│   │   ├── redis.go           # Redis implementation
│   │   └── seeder.go          # Pre-seeds test users
│   ├── utils/                 # UUID, address, hash generation
│   └── webhook/               # Webhook delivery system
│       ├── manager.go         # Webhook manager (enqueue, send, sign)
│       ├── queue.go           # Redis sorted-set job queue
│       ├── worker.go          # Background worker (polls every 5s)
│       └── job.go             # Job struct and serialization
├── features/                  # Gherkin BDD feature files (8 files, ~55 scenarios)
├── test/integration/          # Go integration tests
├── testenv/                   # Godog E2E test runner with Docker Compose
│   ├── docker-compose.yml     # Redis (26380) + MockGatehub (25151)
│   ├── godog_test.go          # BDD runner (//go:build e2e)
│   └── *_steps.go             # Step definitions per feature area
├── web/                       # Static HTML assets
│   ├── index.html             # Deposit/withdraw iframe
│   └── kyc-iframe.html        # KYC onboarding iframe
├── docs/                      # GateHub API reference docs
├── Dockerfile                 # Multi-stage Docker build
├── Makefile                   # Build, test, lint, clean targets
├── .releaserc.json            # Semantic-release configuration
├── go.mod
├── README.md
└── AGENTS.md                  # Comprehensive agent development guide
```

## Critical Constraints

1. **API Compliance**: MockGatehub must be a drop-in GateHub replacement. Applications expect exact API compliance.
2. **Sandbox Parity Only**: Focus on happy paths and sandbox behavior. Production features out of scope.
3. **Multi-Currency Required**: Support all 11 currencies (XRP, USD, EUR, GBP, ZAR, MXN, SGD, CAD, EGG, PEB, PKR).
4. **Immutable Vault UUIDs**: Vault IDs are hardcoded and must never change (applications may store these).
5. **Redis Required for Webhooks**: Even in-memory storage mode requires Redis for the webhook job queue.
6. **testenv/ Maintenance**: The `testenv/` directory is NOT optional. When making changes:
   - Add test cases to feature files and step definitions for new endpoints
   - Update assertions if API responses change
   - Ensure backward compatibility (applications depend on exact GateHub response format)

## Key Architecture

### Handler Struct

```go
type Handler struct {
    store          storage.Storage
    webhookManager *webhook.Manager
    tokenToUser    sync.Map     // Maps bearer tokens → user UUIDs
    feeConfig      *FeeConfig   // Thread-safe fee percentages (default 0%)
}
```

### Storage Layer
- Interface-based design (~30 methods): users, wallets, transactions, balances, cards, customers, accounts, 3DS challenges
- Memory implementation: `sync.RWMutex` for thread safety
- Redis implementation: JSON serialization, atomic balance operations (`INCRBYFLOAT`)
- Seeder: pre-creates two test users with 10,000 USD and 10,000 EUR

### Authentication (HMAC Signatures)
- **Request signatures**: `HMAC-SHA256(timestamp|method|full_url|body, secret)`, hex-encoded, empty segments stripped
- **Webhook signatures**: `HMAC-SHA256(json_body, hex_decoded_secret)` — different algorithm
- Request headers: `x-gatehub-app-id`, `x-gatehub-timestamp`, `x-gatehub-signature`
- Global middleware with public endpoint whitelist: `/health`, `/`, `/iframe/onboarding`, `/iframe/submit`, `/transaction/complete`, `/api/user-currencies`, `/admin/fees`

### Webhook System
- Redis-backed sorted-set job queue with background worker polling every 5s
- `SendAsync(eventType, userID, data, offsetDelaySeconds)` enqueues to queue
- 10 max attempts, 30-second fixed retry backoff
- Configurable minimum delay (`WEBHOOK_MIN_DELAY_SEC`, clamped to ≥2s)
- Payload includes: `uuid`, `timestamp` (ms string), `event_type`, `user_uuid`, `environment` ("sandbox"), `data`

### Transaction Lifecycle
1. `POST /core/v1/transactions` creates transaction in `pending` (status `1`)
2. Pending webhook fires immediately
3. After ~2s delay: marks `completed` (status `100`), credits balance, sends completed webhook
4. Without webhook URL: completes synchronously

Transaction types: `0`=Withdrawal, `1`=Deposit, `2`=Hosted
Transaction statuses: `1`=Pending, `100`=Completed, `3`=Failed

### Fee System
- Thread-safe `FeeConfig` (default 0% deposit, 0% withdrawal)
- Admin API: `GET/PUT /admin/fees` (validates 0–100 range)
- Deposits: net = amount - fee; Withdrawals: total = amount + fee; Hosted: always 0%

### KYC Flow
1. `POST /id/v1/users/{userID}/hubs/{gatewayID}` → `action_required`
2. `GET /iframe/onboarding?bearer={token}` → serves KYC iframe HTML
3. `POST /iframe/submit` → parses form, sets `accepted`, triggers webhook with 2s delay
4. Iframe posts `{ type: 'OnboardingCompleted', value: '...' }` to parent window

## Environment Variables

| Env Var | Default | Description |
|---------|---------|-------------|
| `MOCKGATEHUB_PORT` | `8080` | HTTP port |
| `LOG_LEVEL` | `info` | Zap log level |
| `MOCKGATEHUB_REDIS_URL` | `""` | Redis connection (enables Redis storage) |
| `MOCKGATEHUB_REDIS_DB` | `0` | Redis database number |
| `WEBHOOK_URL` | `""` | Webhook delivery target |
| `WEBHOOK_SECRET` | `mock-secret` | Webhook HMAC signing secret |
| `WEBHOOK_MIN_DELAY_SEC` | `0.05` → clamped ≥ `2` | Minimum webhook delivery delay |
| `MOCKGATEHUB_ENFORCE_AUTHENTICATION` | `true` | Enable/disable HMAC middleware |
| `MOCKGATEHUB_VALID_CREDENTIALS` | `local-test-app-id:local-test-app-secret` | `appId:secret,appId2:secret2` |

## Development Workflow

### Running Locally

```bash
# With Redis (required for webhooks)
MOCKGATEHUB_REDIS_URL=redis://localhost:6379 go run ./cmd/mockgatehub

# Disable auth for quick testing
MOCKGATEHUB_ENFORCE_AUTHENTICATION=false MOCKGATEHUB_REDIS_URL=redis://localhost:6379 go run ./cmd/mockgatehub
```

### Testing

```bash
# All tests (unit + E2E)
make test

# Unit tests only
make unit-tests

# BDD E2E tests (Docker required)
make e2e-tests

# Coverage report
make coverage
```

### Docker Build & Test

```bash
docker build -t local-mockgatehub .
make e2e-tests
```

## CI/CD Pipeline

### GitHub Actions Workflows

**pr-validation.yml**: Runs on pull requests
- PR title validation (Conventional Commits)
- Unit and E2E tests
- Docker build (no push) with GHA cache

**release.yml**: Runs on push to main
- All tests
- semantic-release to determine version and create tag
- Docker build and push to `ghcr.io/interledger/mockgatehub` (linux/amd64 + linux/arm64)

### Semantic Versioning

- `feat` → minor release
- `fix`, `perf`, `docs`, `refactor`, `build`, `ci` → patch release
- `chore`, `test` → no release
- Tag format: `v${version}`
- Config: `.releaserc.json`

## When Adding New Endpoints

1. Define models in `internal/models/api.go` or `internal/models/cards.go`
2. Implement handler in `internal/handler/{domain}.go`
3. Register route in `cmd/mockgatehub/main.go` `setupRoutes()`
4. Write unit tests in `internal/handler/{domain}_test.go`
5. Add BDD scenarios in `features/{domain}.feature` and step definitions in `testenv/{domain}_steps.go`
6. Update `README.md` API section

## When Modifying Storage

1. Update `internal/storage/interface.go`
2. Implement in **both** `memory.go` AND `redis.go`
3. Add tests covering new functionality
4. Update seeder if affecting test user creation

## When Changing Constants

1. Update `internal/consts/consts.go`
2. **NEVER** change existing vault UUIDs (immutable)
3. Update all test files with hardcoded values
4. Update README.md tables

## Logging

It's safe to log sensitive values — this is a development mock, not production.

Use zap structured logging:
```go
logger.Info("deposit created",
    zap.String("user_id", userID),
    zap.String("amount", amountStr),
    zap.String("currency", currency),
)
```

Levels: `Info` (normal ops), `Warn` (non-fatal issues), `Error` (failures), `Debug` (diagnostics)

## Testing Checklist

- [ ] All tests pass: `make test`
- [ ] Coverage acceptable: `make coverage` (aim for ≥80%)
- [ ] Docker build succeeds: `docker build -t local-mockgatehub .`
- [ ] Application works with MockGatehub

## Key Files Reference

**Must review before coding**:
1. `internal/consts/consts.go` — all constants
2. `internal/storage/interface.go` — storage contract
3. `internal/models/models.go` — domain models
4. `cmd/mockgatehub/main.go` — routing configuration

**Frequently modified**:
1. `internal/handler/*.go` — API endpoint implementations
2. `internal/storage/memory.go` — in-memory storage logic
3. `internal/webhook/manager.go` — webhook delivery

## Critical Notes for AI Agents

1. **ALWAYS run all tests after changes**: `make test`
2. **Maintain API compatibility**: Applications rely on exact GateHub response format
3. **Never modify vault UUIDs or currency codes**: These are immutable
4. **Test both storage backends**: Changes must work with memory AND Redis
5. **Update AGENTS.md if you discover outdated guidance**: Keep instructions fresh
6. **Use zap structured logging**: Always include context fields for debugging
7. **Webhook queue requires Redis**: Even in in-memory storage mode

## Troubleshooting

**Tests failing with Redis**: Ensure Redis running and DB is clean (`redis-cli -n 1 FLUSHDB`)

**Docker build fails**: Check `go.mod` and `go.sum` present, verify no syntax errors (`go build ./...`)

**Webhooks not arriving**: Check `WEBHOOK_URL`, verify app backend running, check Docker logs

**E2E tests fail**: Ensure Docker is running, port 25151 and 26380 are free

---

**Reference**: See [AGENTS.md](AGENTS.md) for comprehensive development guide with detailed architecture, examples, and troubleshooting.
