# MockGatehub — AI Agent Development Guide

Guidance for AI coding agents working on MockGatehub. Read the "Critical
Constraints" and "Two Listeners" sections before changing anything.

## Project Context

MockGatehub is a Go mock of the GateHub API. It exists so wallet applications
that integrate with GateHub can be developed and tested locally: no real
credentials, no rate limits, and predictable behaviour in CI.

### Critical Constraints

1. **This is a published contract.** Downstream projects consume MockGatehub as
   a container image (`ghcr.io/interledger/mockgatehub`). Every endpoint,
   response field and status code is a public API. Renaming a route, narrowing a
   method or dropping a field breaks a consumer you cannot see from here.
   `features/consumer_contract.feature` pins the paths and fields real consumers
   depend on — if a change makes it fail, the change is wrong, not the test.
2. **API compliance over elegance.** Applications expect GateHub's exact
   response format, including its inconsistencies. Match the real API rather
   than tidying it.
3. **Sandbox parity only.** Happy paths and sandbox behaviour. Production
   GateHub features are out of scope.
4. **Multi-currency.** All 11 currencies: XRP, USD, EUR, GBP, ZAR, MXN, SGD,
   CAD, EGG, PEB, PKR.
5. **Vault UUIDs are immutable.** They are hardcoded in
   `internal/consts/consts.go` and stored in consumer databases. Never change
   them.
6. **Redis is optional.** Storage falls back to memory and webhook delivery is
   simply disabled without it. Do not reintroduce a hard Redis dependency at
   startup.

## Two Listeners

MockGatehub serves two ports, and the split is load-bearing.

| Port | Serves | Auth |
|------|--------|------|
| `MOCKGATEHUB_PORT` (`8080`) | GateHub API, iframes, browser-facing card-data and PIN endpoints | HMAC, minus a small public allowlist |
| `MOCKGATEHUB_ADMIN_PORT` (`8081`) | Admin UI (`/ui`) and test-support endpoints (`/admin/*`, `/test-webhook`) | **None** |

The admin surface has no authentication and several of its endpoints mutate
state, so it is protected by *not being reachable* rather than by being
authenticated. It is registered on a **separate router**, so admin paths return
404 on the application port even when `MOCKGATEHUB_ENFORCE_AUTHENTICATION=false`.

- Routes go in `setupRoutes()` **or** `setupAdminRoutes()` in
  `cmd/mockgatehub/main.go` — never both.
- Anything that is not part of the GateHub API belongs on the admin listener.
- The process refuses to start if the two ports are equal.
- `/health` is served on both.

## Architecture

### Tech Stack

- **Go 1.24+**, chi v5 router, Zap structured logging
- **Storage**: dual backend — in-memory and Redis, behind one interface
- **Webhooks**: Redis Streams consumer group with a blocking worker
- **Testing**: testify (unit), godog/Cucumber (BDD E2E against real containers)
- **Lint**: golangci-lint, standard linter set, `errcheck` and `staticcheck` on
- **CI**: GitHub Actions with semantic-release

### Layout

```
cmd/mockgatehub/main.go        Entry point: config, storage, webhooks, both routers
internal/
  auth/                       HMAC signature generation, verification, middleware
  config/                     Env var loading (config.Load)
  consts/                     Currencies, vault IDs, statuses, webhook events
  handler/
    handler.go                Handler struct, RequestLogger, health, iframe roots
    helpers.go                sendJSON/sendError, formatAmount, tokenPrefix
    apierror.go               apiError: couples a failure with its HTTP status
    auth.go                   /auth/v1
    identity.go               /id/v1, KYC iframe submit, 2FA callback
    core.go                   /core/v1 wallets and transactions
    cards.go                  /cards/v1 card lifecycle
    card_token.go             Card token JWTs, RSA encryption, service key pair
    cardscenarios.go          Embedded card transaction catalogue loader
    cardtx_admin.go           Card transaction simulation
    withdrawals_admin.go      Withdrawal listing and settlement
    statements.go             /statement/v1 PDF statements
    ui.go                     Admin UI handlers
    webhook_sink.go           Test-support webhook receiver
    rates.go, fees.go         Rates, vaults, fee configuration
    mockdata/                 cardtransaction_mocks.json (17 scenarios)
    web/ui/                   Embedded admin UI templates
  logger/                     Zap dual-core logger
  models/                     Domain models and API DTOs
  pdf/                        Minimal single-page PDF renderer (statements)
  storage/                    interface.go + memory.go + redis.go + seeder.go
  utils/                      UUID, mock XRPL address, transaction hash
  webhook/                    manager.go, queue.go (Streams), worker.go, job.go
features/                     Gherkin BDD features (13 files)
testenv/                      Godog runner, step definitions, docker-compose
test/integration/             Go integration tests
web/                          index.html, deposit.html, withdrawal.html, kyc-iframe.html
docs/                         GateHub API reference and the interledger-app port plan
```

### Design Principles

1. **Dependency injection** — the handler receives storage, webhook manager and
   config via its constructor.
2. **Interface-based storage** — memory and Redis are interchangeable.
3. **One code path per operation** — the admin UI calls the same functions the
   HTTP endpoints call (`simulateCardTransaction`, `settleWithdrawal`) so the two
   cannot drift.
4. **Minimal dependencies** — chi, redis, uuid, testify, zap, godog.

## Core Systems

### Configuration (`internal/config/`)

| Env Var | Default | Notes |
|---------|---------|-------|
| `MOCKGATEHUB_PORT` | `8080` | Application API port |
| `MOCKGATEHUB_ADMIN_PORT` | `8081` | Admin UI and test-support port; must differ from the above |
| `LOG_LEVEL` | `info` | Zap log level |
| `MOCKGATEHUB_REDIS_URL` | `""` | Enables Redis storage when set |
| `MOCKGATEHUB_REDIS_DB` | `0` | Redis database number |
| `WEBHOOK_URL` | `""` | Webhook delivery target |
| `WEBHOOK_SECRET` | `mock-secret` | Webhook HMAC signing secret |
| `WEBHOOK_MIN_DELAY_SEC` | `0.05` | Minimum delay before delivery; no longer clamped |
| `MOCKGATEHUB_ENFORCE_AUTHENTICATION` | `true` | HMAC middleware on the application listener |
| `MOCKGATEHUB_VALID_CREDENTIALS` | `local-test-app-id:local-test-app-secret` | `appId:secret,appId2:secret2` |
| `DEFAULT_ORGANIZATION_ID` | `default-org` | Organization used for callback routing |
| `MOCKGATEHUB_PUBLIC_BASE_URL` | `http://localhost:{PORT}` | Base for absolute links a browser follows; derived from the port when unset |
| `MOCKGATEHUB_CARD_DATA_TOKEN_SECRET` | random per process | Signs card-data and PIN JWTs; never hardcode a default |
| `MOCKGATEHUB_ASYNC_WITHDRAWALS` | `false` | Opt-in: withdrawals stay pending until settled |

### Storage (`internal/storage/`)

`interface.go` defines ~50 methods across users, card customers, accounts,
addresses, cards, card limits, card PINs, card transactions (typed and raw, plus
a sequence counter), wallets, transactions, balances, 3DS challenges and
organizations.

- **memory.go** — `sync.RWMutex` and maps.
- **redis.go** — JSON values, `INCRBYFLOAT` for balances, keys like
  `user:{id}`, `wallet:{address}`, `balance:{userID}:{currency}`.
- **seeder.go** — two test users, `...0001` with 10,000 USD and `...0002` with
  10,000 EUR, both KYC `action_required`.

Every interface change must be implemented in **both** backends and tested
against both. `internal/storage/cardtx_test.go` shows the pattern: it runs each
expectation against memory and against Redis, skipping Redis when unavailable.

### Authentication (`internal/auth/`)

- **Request signature**: `HMAC-SHA256(timestamp|method|full_url|body, secret)`,
  hex. Empty segments stripped. Full URL is reconstructed from
  `X-Forwarded-Proto`/`X-Forwarded-Host` behind a proxy.
- **Webhook signature**: a *different* algorithm —
  `HMAC-SHA256(json_body, hex_decoded_secret)` via
  `GenerateGateHubWebhookSignature`.
- **Public endpoints** (application listener): `/health`, `/`,
  `/iframe/onboarding`, `/iframe/submit`, `/transaction/complete`,
  `/api/user-currencies`, and the browser-facing card token data paths
  (`/cards/v1/token/card-data/data`, `/cards/v1/token/pin/data`,
  `/cards/v1/token/pin/public-key`), which a browser reaches holding only a
  short-lived token.
- The middleware runs **before** routing, so an unrouted path returns 401 rather
  than 404 while auth is enforced. Keep that in mind when probing.

### Webhooks (`internal/webhook/`)

- **queue.go** — Redis Streams consumer group. Jobs are added to a stream and
  read with `XREADGROUP`.
- **worker.go** — blocks on the stream rather than polling, so a webhook is
  delivered as soon as it is enqueued. Reclaims stale messages left by a crashed
  worker.
- **manager.go** — `SendAsync(eventType, userID, data, offsetDelaySeconds)`
  enqueues; `send()` delivers with HMAC signing. `SendAsync` is already
  asynchronous — do not wrap it in `go`.
- **Retry**: 10 attempts, 3-second backoff.
- **Callback routing**: organization `apiBaseUrl` first, then `WEBHOOK_URL`.

**Events** (`internal/consts/consts.go`):
`id.verification.accepted` / `.rejected` / `.action_required` / `.resubmission`,
`id.document_notice.warning` / `.expired`, `core.deposit.completed`,
`core.withdrawal.completed`, `more-bridge.withdrawal.rejected`,
`cards.card.created`, `cards.transaction.authorization`,
`cards.transaction.event`, `cards.3ds.auth_3ds_confirmation`.

Two things worth knowing:

- Only the three terminal verification outcomes get a synthesised `verified`
  summary. A resubmission request and the document notices are not verdicts, and
  attaching one would tell a consumer verification had concluded.
- `cards.transaction.authorization` carries the whole transaction under
  `authorizationData` and is what consumers listen for.
  `cards.transaction.event` carries identifiers only.

**Payload**:
```json
{"uuid":"...","timestamp":"1768920404045","event_type":"core.deposit.completed",
 "user_uuid":"...","environment":"sandbox","data":{}}
```

### Handler (`internal/handler/`)

```go
type Handler struct {
    config         *config.Config
    store          storage.Storage
    webhookManager *webhook.Manager
    httpClient     *http.Client
    tokenToUser    sync.Map   // bearer token → user UUID
    feeConfig      *FeeConfig
}
```

`NewHandlerWithConfig(cfg, store, wm)` is the real constructor; `NewHandler`
loads config from the environment for convenience. Operations shared between the
HTTP API and the admin UI return an `apiError` carrying the status, so neither
caller owns the status codes.

### Cards

- **Token flow**: `POST /cards/v1/token/{tokenType}` keeps its wildcard —
  consumers call it as `card-data`, `pin` and `pin-change`. Narrowing it breaks
  two of the three. `card-data` and `pin` mint a signed, type-scoped,
  short-lived JWT and advertise an absolute link; unimplemented types keep a
  placeholder response.
- **Encryption**: PKCS#1 v1.5, because consumers decrypt with
  `encryptionScheme: 'pkcs1'`. OAEP would fail.
- **Catalogue**: 17 realistic transaction payloads in
  `internal/handler/mockdata/cardtransaction_mocks.json`, keyed as
  `operation.classification.case`. Identity fields (`transactionId`, `id`,
  `cardId`) are always assigned by this service, never taken from the fixture.
- **`models.CardTransaction`** must carry a numeric `id` and `cardId` —
  consumers key off both.

### Fees (`internal/handler/fees.go`)

`CalculateFee(amount, percent) = round(amount * percent / 100, 2)`. Deposits
credit `amount - fee`; withdrawals deduct `amount + fee`; hosted transfers are
always 0%.

### Transactions

1. `POST /core/v1/transactions` creates the transaction as pending (status `1`).
2. It completes asynchronously, crediting or debiting the balance, and emits a
   single `core.deposit.completed` webhook. There is **no** pending webhook —
   that was removed deliberately to match real behaviour.
3. Direction for a hosted transfer comes from whether `sending_address` resolves
   to a wallet the user owns: if so it debits, otherwise it credits.

**Types**: `0` withdrawal, `1` deposit, `2` hosted.
**Statuses**: `1` pending, `100` completed, `3` failed.

### Withdrawals

Immediate by default: created completed, balance charged, no webhook. With
`MOCKGATEHUB_ASYNC_WITHDRAWALS=true` they stay pending and nothing is charged
until settled via `POST /admin/withdrawals/{txID}/trigger-event`. The balance
moves at settlement so a rejection leaves it untouched, and settling is
once-only.

### KYC

1. `POST /id/v1/users/{userID}/hubs/{gatewayID}` → `action_required`.
2. `GET /iframe/onboarding?bearer={token}` serves `web/kyc-iframe.html`.
3. `POST /iframe/submit` accepts an optional `kyc_outcome`
   (`accepted` | `rejected` | `action_required` | `resubmission`), defaulting to
   accepted. An unrecognised value is refused rather than treated as a pass.
4. The iframe posts `OnboardingCompleted` to the parent window.

`PUT /admin/users/{userID}/kyc-state` sets a state without emitting a webhook,
for arranging a starting state in a test.

## Testing

| Location | Type |
|----------|------|
| `internal/*/` | Unit |
| `test/integration/` | Go integration |
| `testenv/` + `features/` | E2E BDD (godog, real containers) |

```bash
make unit-tests   # unit + integration
make e2e-tests    # builds Docker, runs godog against containers
make test         # lint + unit + e2e
make lint         # gofmt (fails on unformatted), go vet, golangci-lint
make coverage
```

### E2E Environment

`testenv/docker-compose.yml` runs Redis on `26380` plus two MockGatehub
instances, so both positions of the async-withdrawals switch are covered:

| Instance | Application | Admin |
|---|---|---|
| default | `25151` | `25153` |
| async withdrawals | `25152` | `25154` |

- Tests are behind the `e2e` build tag.
- `GODOG_TAGS='@cards' go test -tags e2e ./testenv/ -run TestFeatures` runs a
  subset. Every feature file carries a tag.
- **`Strict: true`** is set, so a scenario whose steps have no definition
  **fails** instead of silently reporting success.
- The harness routes `/admin/*`, `/ui` and `/test-webhook` to the admin listener
  automatically (`urlFor` in `http_client.go`); step definitions use plain paths.
- `POST /test-webhook` records deliveries and verifies their signature;
  `GET /admin/received-webhooks` inspects them. This is how webhook behaviour is
  asserted — do not write a webhook assertion that only checks an HTTP status.

### Feature Files

13 files, ~148 scenario declarations, 166 executed once outlines expand:
`service_health`, `auth_user_kyc`, `wallets_and_balances`, `transactions`,
`rates_and_vaults`, `cards`, `signature_authentication`, `fee_configuration`,
`organization_configuration`, `statements`, `withdrawals`, `admin_ui`,
`consumer_contract`.

### Writing E2E Tests

Assert on **observable behaviour**, not on the fact that a request returned 200.
A step named "the balance increases by 75.50" must read the balance; there is
history in this repo of such a step only checking a status code.

Prove a new test can fail. Break the behaviour deliberately, watch the suite go
red, then restore it. Several bugs were caught this way, and one assertion was
found to be vacuous.

## CI/CD

- **`pr-validation.yml`** — Conventional Commits title check, unit and E2E
  tests, Docker build without push.
- **`release.yml`** on push to `main` — tests, semantic-release, multi-arch
  Docker push to `ghcr.io/interledger/mockgatehub`.
- **Versioning**: `feat` → minor; `fix`/`perf`/`docs`/`refactor`/`build`/`ci` →
  patch; `chore`/`test` → none. Config in `.releaserc.json`.

## Logging

Logging sensitive values is acceptable here — this is a mock, not production.
Use structured fields:

```go
logger.Info("deposit created",
    zap.String("user_id", userID),
    zap.String("amount", amountStr),
    zap.String("currency", currency))
```

`Info` normal operations · `Warn` non-fatal issues · `Error` failed operations ·
`Debug` detailed diagnostics. Use `tokenPrefix()` for tokens rather than slicing
by hand.

## Recipes

### Adding an endpoint

1. Models in `internal/models/api.go` or `cards.go`.
2. Handler in `internal/handler/{domain}.go`.
3. Register in `setupRoutes()` for the GateHub API, or `setupAdminRoutes()` if it
   is test support.
4. Unit tests in `internal/handler/`.
5. BDD scenarios in `features/` plus step definitions in `testenv/`.
6. Update `README.md`, and `features/consumer_contract.feature` if a consumer
   depends on it.

### Modifying storage

1. Update `internal/storage/interface.go`.
2. Implement in **both** `memory.go` and `redis.go`.
3. Test against both backends.
4. Update `seeder.go` if it affects the seeded users.

### Changing constants

1. Update `internal/consts/consts.go`.
2. **Never** change a vault UUID or currency code.
3. Update tests with hardcoded values, and the README tables.

### Checklist

- [ ] `make lint` — zero findings
- [ ] `make test` — unit and E2E green
- [ ] Both storage backends exercised
- [ ] `features/consumer_contract.feature` still passes
- [ ] New behaviour has an E2E scenario that fails when the behaviour is broken
- [ ] `docker build -t local-mockgatehub .` succeeds

## Key Files

**Read before coding**: `internal/consts/consts.go`,
`internal/storage/interface.go`, `internal/models/models.go`,
`cmd/mockgatehub/main.go`, `features/consumer_contract.feature`.

**Frequently modified**: `internal/handler/*.go`,
`internal/storage/memory.go`, `internal/webhook/manager.go`.

**Background**: `docs/interledger-app-port-plan.md` records how the
interledger-app fork was ported, the defects found on both sides, and the
consumer contract that constrains this repo.

## Troubleshooting

**Redis tests skipping** — storage tests use DB 15; start Redis with
`docker run -d --rm -p 6379:6379 redis:7-alpine`.

**E2E fails to start** — Docker must be running and ports `25151`, `25152`,
`25153`, `25154`, `26380` free.

**Webhooks not arriving** — check `WEBHOOK_URL`, then
`GET /admin/received-webhooks` on the admin port, then the container logs
(`testenv/lastlogs.txt` after a run).

**Admin endpoint 404s** — it is on the admin port (`8081`), not `8080`.

## Critical Notes

1. **Run `make test` after every change.** Lint is part of it.
2. **Maintain API compatibility.** Consumers rely on the exact response format.
3. **Never modify vault UUIDs or currency codes.**
4. **Test both storage backends.**
5. **Application routes and admin routes live on different listeners.**
6. **Do not wrap `SendAsync` in `go`** — it is already asynchronous.
7. **Update this file when you find it stale**, and say so in the commit.

---

**Last Updated**: August 2026
**Maintainers**: Interledger Foundation
