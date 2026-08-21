# MockGatehub

EXPERIMENTAL

A lightweight Go mock of the GateHub API for local development and testing of wallet applications that integrate with GateHub.

Official GateHub documentation can be found [here](https://docs.gatehub.net/api-documentation/c3OPAp5dM191CDAdwyYS).

## Overview

MockGatehub provides a drop-in replacement for GateHub's sandbox environment, enabling developers to:

- Develop and test wallet integrations without real GateHub credentials
- Run locally without external API dependencies
- Test multi-currency operations (11 supported currencies)
- Verify KYC flows with a realistic iframe and server-side approval
- Test webhook delivery mechanisms
- UNSTABLE - Test card issuance, transactions, and 3DS challenge flows
- UNSTABLE - Configure transaction fees for deposit/withdrawal testing

## Features

- **Full API Coverage**: Authentication, KYC, wallets, transactions, rates, fees, and cards
- **Multi-Currency Support**: XRP, USD, EUR, GBP, ZAR, MXN, SGD, CAD, EGG, PEB, PKR
- **Realistic KYC Flow**: Iframe-based form with `action_required` → `accepted` lifecycle
- **Card Services**: Full card lifecycle — issuance, lock/unlock/block, limits, transactions, 3DS challenges
- **Encrypted Card Data**: Card details and PIN returned RSA-encrypted to the caller's own key, via short-lived signed tokens
- **Card Transaction Catalogue**: 17 realistic GateHub transaction payloads — purchases, FX, ATM, cash advance, pre-authorizations, reversals and declines — simulatable against a real card
- **Statements**: Account confirmation, monthly statement and transfer confirmation, rendered as PDFs describing the account they belong to
- **Admin UI**: A browser UI at `/ui` for driving KYC events, card transactions and withdrawal settlement without writing requests by hand
- **Webhook Delivery**: Redis-backed job queue with configurable delay, retry, and HMAC signing
- **Configurable Fees**: Runtime-adjustable deposit and withdrawal fee percentages via admin API
- **Dual Storage**: In-memory (development) and Redis (runtime) backends
- **HMAC Authentication**: Enforced by default, matching real GateHub signature validation
- **Pre-seeded Users**: Test users with balances ready to use
- **BDD Test Suite**: 144 Gherkin scenarios run with godog against real containers, including a consumer-contract suite that pins every endpoint downstream integrations call

## Quick Start

### Running with Docker Compose

```bash
docker compose up -d mockgatehub
```

The service will be available at `http://localhost:8080`

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MOCKGATEHUB_PORT` | `8080` | HTTP server port |
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `MOCKGATEHUB_REDIS_URL` | — | Redis connection URL (enables Redis storage) |
| `MOCKGATEHUB_REDIS_DB` | `0` | Redis database number |
| `MOCKGATEHUB_ENFORCE_AUTHENTICATION` | `true` | Enable HMAC signature validation |
| `MOCKGATEHUB_VALID_CREDENTIALS` | `local-test-app-id:local-test-app-secret` | Comma-separated `appId:secret` pairs |
| `WEBHOOK_URL` | — | Application webhook endpoint URL (fallback if no org config) |
| `WEBHOOK_SECRET` | `mock-secret` | Secret for signing outgoing webhooks |
| `WEBHOOK_MIN_DELAY_SEC` | `0.05` | Minimum seconds before webhooks become eligible for delivery (clamped to a 2s floor) |
| `WEBHOOK_POLL_INTERVAL_MS` | `5000` | How often the delivery worker polls for due webhooks |
| `WEBHOOK_BATCH_SIZE` | `10` | Webhooks delivered per poll. With the default pacing, a burst of webhooks drains at 2/second; lower the interval in a test environment |
| `DEFAULT_ORGANIZATION_ID` | `default-org` | Organization ID for callback routing |
| `MOCKGATEHUB_PUBLIC_BASE_URL` | `http://localhost:8080` | Externally reachable base URL, used for absolute links a browser follows directly (card-data and PIN) |
| `MOCKGATEHUB_CARD_DATA_TOKEN_SECRET` | random per process | HMAC secret signing card-data and PIN tokens. Set it only if tokens must survive a restart |
| `MOCKGATEHUB_ASYNC_WITHDRAWALS` | `false` | When enabled, withdrawals stay pending until settled through the admin API instead of completing immediately. See [Withdrawal Settlement](#withdrawal-settlement) |

> **Note**: Redis is optional. Without it, storage is in-memory and webhook
> delivery is disabled — enough for exploring the API, but webhook-driven flows
> need `MOCKGATEHUB_REDIS_URL` set.

### Pre-seeded Test Users

Two test users are automatically created at startup:

| | User 1 | User 2 |
|-|--------|--------|
| **Email** | `testuser1@mockgatehub.local` | `testuser2@mockgatehub.local` |
| **User ID** | `00000000-0000-0000-0000-000000000001` | `00000000-0000-0000-0000-000000000002` |
| **Balance** | 10,000 USD | 10,000 EUR |
| **KYC State** | `action_required` | `action_required` |

## API Endpoints

### Health & Utility

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Service health status |
| `GET` | `/api/user-currencies` | Currencies with non-zero balances for a user |

### Authentication (`/auth/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tokens` | Generate iframe access token |
| `POST` | `/users/managed` | Create managed user |
| `GET` | `/users/managed` | Get managed user by email |
| `PUT` | `/users/managed/email` | Update user email |
| `PATCH` | `/users/organization/{orgID}` | Update organization configuration |

### Identity / KYC (`/id/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/users/{userID}` | Get user state and profile |
| `POST` | `/users/{userID}/hubs/{gatewayID}` | Start KYC process |
| `PUT` | `/hubs/{gatewayID}/users/{userID}` | Update KYC state |
| `POST` | `/hubs/{gatewayID}/users/{userID}/overrideRiskLevel` | Override risk level |

### Iframe Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/iframe/onboarding` | Serve KYC iframe HTML |
| `POST` | `/iframe/submit` | KYC iframe form submission |
| `GET` | `/` | Serve deposit/withdrawal iframe |
| `POST` | `/transaction/complete` | Iframe transaction completion callback |

### Wallets & Transactions (`/core/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/users/{userID}` | Get user wallets (auto-creates if none) |
| `POST` | `/users/{userID}/wallets` | Create new wallet |
| `GET` | `/users/{userID}/wallets/{walletID}` | Get wallet details |
| `GET` | `/wallets/{walletID}/balances` | Get multi-currency balance |
| `POST` | `/transactions` | Create deposit/hosted/withdrawal transaction |
| `GET` | `/transactions/{txID}` | Get transaction details |

### Rates (`/rates/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/rates/current` | Get current exchange rates |
| `GET` | `/liquidity_provider/vaults` | Get vault UUIDs |

### Fee Configuration (Admin)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/fees` | Get current fee percentages |
| `PUT` | `/admin/fees` | Set deposit/withdrawal fee percentages (0–100) |

### Cards (`/cards/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/customers` | Create customer |
| `POST` | `/customers/managed` | Create managed customer with account and card |
| `POST` | `/customers/{customerID}/addresses` | Add delivery address |
| `GET` | `/customers/{customerID}/addresses` | List delivery addresses |
| `POST` | `/cards` | Create card |
| `GET` | `/cards/{customerID}` | List cards for customer |
| `GET` | `/cards/{cardID}/card` | Get card details |
| `DELETE` | `/cards/{cardID}/card` | Delete (soft-delete) card |
| `PUT` | `/cards/{cardID}/lock` | Lock card temporarily |
| `PUT` | `/cards/{cardID}/unlock` | Unlock card |
| `PUT` | `/cards/{cardID}/block` | Block card permanently |
| `GET` | `/cards/{cardID}/limits` | Get card spending limits |
| `PUT` | `/cards/{cardID}/limits` | Update card spending limits |
| `POST` | `/token/card-data` | Generate card data token |
| `POST` | `/accounts/{accountID}/cards` | Order additional card |
| `POST` | `/cards/{cardID}/plastic` | Order physical plastic card |
| `POST` | `/transactions` | Create card transaction |
| `GET` | `/transactions/{txID}` | Get card transaction |
| `GET` | `/cards/{cardID}/transactions` | List card transactions |
| `GET` | `/transaction/pending-confirmations` | List pending 3DS challenges |
| `POST` | `/test/3ds/challenge` | Create 3DS challenge (testing) |
| `POST` | `/transaction/{txID}` | Confirm/decline 3DS challenge |
| `GET` | `/card-applications/{appID}/card-products` | Get card product catalog |
| `POST` | `/token/{tokenType}` | Mint a short-lived token (`card-data`, `pin`, `pin-change`) |
| `GET` | `/token/card-data/data` | Card PAN, expiry and CVC, RSA-encrypted to the caller's key |
| `GET` | `/token/pin/data` | Card PIN, RSA-encrypted to the caller's key |
| `POST` | `/token/pin/data` | Set a new PIN, supplied encrypted to this service's key |
| `GET` | `/token/pin/public-key` | The key a caller encrypts a new PIN to |

Card limits, the product catalog and a customer's card list are also served
under a bare `/v1` prefix (`/v1/cards/{cardID}/limits`,
`/v1/card-applications/{appID}/card-products`,
`/cards/v1/customers/{customerID}/cards`), matching how GateHub exposes them.

### Statements (`/statement/v1/`)

Each returns a PDF describing the account or transaction it belongs to, rather
than one indistinguishable placeholder.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/statements/account-confirmation/{walletAddress}` | Confirmation that an account exists |
| `GET` | `/statements/account-statement/{walletAddress}/{year}/{month}` | One month's activity |
| `GET` | `/statements/transfer-confirmation/{transactionUUID}` | Confirmation of one deposit or withdrawal |

### Test Support (Admin)

Not part of the GateHub API. These exist so an automated test can arrange state
and observe what was emitted.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `PUT` | `/admin/users/{userID}/kyc-state` | Set a KYC state without emitting a webhook |
| `GET` | `/admin/card-transactions/scenarios` | List the card transaction catalogue |
| `POST` | `/admin/card-transactions/simulate` | Materialise a scenario against a card and emit its webhook |
| `POST` | `/admin/card-transactions/{txID}/status` | Advance a card transaction's status |
| `GET` | `/admin/users/{userID}/withdrawals` | List withdrawals, optionally by status |
| `POST` | `/admin/withdrawals/{txID}/trigger-event` | Settle a pending withdrawal |
| `POST` | `/test-webhook` | Webhook sink; records deliveries and verifies their signature |
| `GET` | `/admin/received-webhooks` | Inspect recorded deliveries |
| `DELETE` | `/admin/received-webhooks` | Clear recorded deliveries |

### Admin UI (`/ui`)

A browser UI for driving the mock by hand. Needs no credentials.

| Path | Description |
|------|-------------|
| `/ui` | Users and their balances |
| `/ui/users/{userID}` | Balances, cards and transactions for one user, with withdrawal settlement |
| `/ui/actions/kyc` | Send a verification event or document notice |
| `/ui/actions/card-transaction` | Simulate a card transaction from the catalogue |

## Supported Currencies

| Currency | Code | Vault UUID |
|----------|------|------------|
| US Dollar | USD | `450d2156-132a-4d3f-88c5-74822547658d` |
| Euro | EUR | `a09a0a2c-1a3a-44c5-a1b9-603a6eea9341` |
| British Pound | GBP | `992b932d-7e9e-44b0-90ea-b82a530b6784` |
| South African Rand | ZAR | `f1c412ce-5e2b-4737-9121-b7c11d6c3f93` |
| Mexican Peso | MXN | `426c2e30-111e-4273-92b3-508445a6bb58` |
| Singapore Dollar | SGD | `e2914c33-2e57-49a5-ac06-25c006497b3d` |
| Canadian Dollar | CAD | `bd5af6fe-5d92-4b20-9bd4-1baa52b7a02e` |
| EGG (Test) | EGG | `9a550347-799e-4c10-9142-f1a2e1c084e7` |
| PEB (Test) | PEB | `0ba2b0d1-b7a2-416c-a4ac-1cb3e5281300` |
| Pakistani Rupee | PKR | `2868b4e5-7178-4945-8ec5-8208fac2a22d` |
| XRP | XRP | `6e1f2a3b-4c5d-6e7f-8a9b-0c1d2e3f4a5b` |

## Webhook Events

MockGatehub delivers webhooks via a Redis-backed job queue with configurable minimum delay, 10 retry attempts, and 30-second fixed retry backoff. Webhooks are signed with `X-GH-Webhook-Signature` using HMAC-SHA256.

Delivery is paced by `WEBHOOK_POLL_INTERVAL_MS` and `WEBHOOK_BATCH_SIZE`. The defaults are unhurried; an environment that generates webhooks in bursts should lower the interval so it is not waiting on the queue.

### Supported Event Types

| Event | Trigger |
|-------|---------|
| `id.verification.accepted` | KYC approved (iframe submit) |
| `id.verification.rejected` | KYC rejected |
| `id.verification.action_required` | KYC requires action |
| `id.verification.resubmission` | KYC documents must be resubmitted |
| `id.document_notice.warning` | An identity document is expiring |
| `id.document_notice.expired` | An identity document has expired |
| `core.deposit.completed` | Deposit/hosted transfer completed |
| `core.withdrawal.completed` | Withdrawal settled (asynchronous withdrawals only) |
| `more-bridge.withdrawal.rejected` | Withdrawal rejected (asynchronous withdrawals only) |
| `cards.card.created` | Card created; reports the assigned customer, account and card ids |
| `cards.transaction.authorization` | Card transaction, carrying the transaction under `authorizationData` |
| `cards.transaction.event` | Card transaction notification, carrying identifiers only |
| `cards.3ds.auth_3ds_confirmation` | 3DS challenge confirmation |

> Consumers that mirror card spend into their own ledger want
> `cards.transaction.authorization`: it carries the transaction itself.
> `cards.transaction.event` carries only identifiers.

The three verification outcomes carry a synthesised `verified` summary. A
resubmission request and the document notices deliberately do not — they are not
verdicts, and summarising them as such would report a verification that has not
concluded.

### Webhook Payload Format

```json
{
  "uuid": "webhook-uuid",
  "timestamp": "1768920404045",
  "event_type": "core.deposit.completed",
  "user_uuid": "user-id",
  "environment": "sandbox",
  "data": { ... }
}
```

> The `timestamp` is milliseconds since epoch as a string. The `environment` is always `"sandbox"`.

## KYC Flow

1. **Start KYC** (`POST /id/v1/users/{userID}/hubs/{gatewayID}`) — sets user to `action_required`
2. **Load iframe** (`GET /iframe/onboarding?bearer={token}`) — serves KYC form from `web/kyc-iframe.html`
3. **Submit form** (`POST /iframe/submit`) — accepts `multipart/form-data` or URL-encoded, updates user to `accepted`, triggers `id.verification.accepted` webhook
4. **Parent notification** — iframe posts `{ type: 'OnboardingCompleted', value: '{"applicantStatus":"submitted"}' }` to the parent window

The `bearer` token is required. The `user_id` form field is optional — if omitted, it is resolved from the token-to-user mapping created via `/auth/v1/tokens`.

### Selecting the outcome

The submit accepts an optional `kyc_outcome` field so a consumer can be
exercised against something other than the happy path:

| `kyc_outcome` | Resulting state | Webhook |
|---|---|---|
| omitted or `accepted` | `accepted` | `id.verification.accepted` |
| `rejected` | `rejected` | `id.verification.rejected` |
| `action_required` | `action_required` | `id.verification.action_required` |
| `resubmission` | `resubmission` | `id.verification.resubmission` |

An unrecognised value is refused with `400` rather than quietly accepted, so a
typo cannot read as a passing verification.

Re-submitting for a user already in `resubmission` moves them to `accepted`
without emitting the accepted webhook, since the consumer drives that transition
itself.

To arrange a starting state without emitting anything, use
`PUT /admin/users/{userID}/kyc-state`.

### Optional 2FA TOTP Verification

The KYC iframe includes an optional **"Trigger 2FA TOTP verification"** checkbox. When checked:

1. User enters a TOTP code in the revealed input field
2. On form submit, MockGateHub calls the integrator's 2FA endpoint before completing KYC:
   ```
   POST {org.apiBaseUrl}/v1/users/managed/{userId}/2fa
   {"action": "VERIFY", "code": "{entered_code}"}
   ```
3. If integrator returns `{"success": true}` → KYC proceeds normally
4. If integrator returns `{"success": false}` or non-2xx → form submission fails with error
5. If no organization `apiBaseUrl` is configured → form submission fails with clear error

This requires the organization configuration to be set via `PATCH /auth/v1/users/organization/{orgID}` with a valid `apiBaseUrl`.

## Transaction Lifecycle

1. `POST /core/v1/transactions` creates a transaction in `pending` state (status `1`)
2. A pending webhook (`core.deposit.completed`) fires immediately
3. After a ~2-second delay, the transaction is marked `completed` (status `100`), the balance is credited, and a completed webhook fires
4. If no webhook URL is configured, the transaction completes synchronously

**Transaction types**: `0` = Withdrawal, `1` = Deposit (external), `2` = Hosted transfer

**Fee behavior**: Deposit fees reduce the credited amount. Withdrawal fees increase the total deducted. Hosted transfers always have 0% fee.

**Hosted transfer direction**: a hosted transfer can move money either way. If
`sending_address` resolves to a wallet the user owns, the transfer debits them;
otherwise it credits them.

## Withdrawal Settlement

By default a withdrawal completes as soon as it is requested: the balance is
charged and the outcome is reported in the response. No webhook is emitted.

Setting `MOCKGATEHUB_ASYNC_WITHDRAWALS=true` switches to the asynchronous model
a real provider uses:

1. The withdrawal is created `pending` and **nothing is charged**. The balance is
   still checked up front, so an unaffordable withdrawal is refused immediately.
2. `GET /admin/users/{userID}/withdrawals?status=pending` finds it.
3. `POST /admin/withdrawals/{txID}/trigger-event` settles it with either
   `core.withdrawal.completed` or `more-bridge.withdrawal.rejected`.
4. Completion charges the balance and emits the webhook; rejection leaves the
   balance untouched. The balance moves at settlement, not at request time, so a
   rejected withdrawal never shows a balance that dropped and came back.
5. Settling is once-only. A second attempt, or completing something already
   rejected, returns `409`.

It is opt-in because a consumer that handles no withdrawal webhooks would
otherwise see its withdrawals never complete.

## Authentication

All API requests require HMAC-SHA256 signatures by default, matching real GateHub behavior.

### Default Test Credentials

- **App ID**: `local-test-app-id`
- **Secret**: `local-test-app-secret`

### Request Signing

Include these headers on every request:

- `x-gatehub-app-id` — application identifier
- `x-gatehub-timestamp` — Unix timestamp (seconds)
- `x-gatehub-signature` — `HMAC-SHA256(timestamp|method|full_url|body, secret)`, hex-encoded

```bash
TIMESTAMP=$(date +%s)
BODY='{"email":"user@example.com"}'
SIGNATURE=$(echo -n "${TIMESTAMP}POST/auth/v1/tokens${BODY}" | openssl dgst -sha256 -hmac "local-test-app-secret" -hex | cut -d' ' -f2)

curl -X POST http://localhost:8080/auth/v1/tokens \
  -H "x-gatehub-app-id: local-test-app-id" \
  -H "x-gatehub-timestamp: $TIMESTAMP" \
  -H "x-gatehub-signature: $SIGNATURE" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

### Public Endpoints (No Auth Required)

`/health`, `/`, `/iframe/onboarding`, `/iframe/submit`, `/transaction/complete`, `/api/user-currencies`, `/admin/fees`, the `/admin/*` test-support endpoints, `/test-webhook`, and all of `/ui`.

The card-data and PIN data endpoints (`/cards/v1/token/card-data/data`,
`/cards/v1/token/pin/data`, `/cards/v1/token/pin/public-key`) are also exempt
from HMAC: a browser reaches them directly holding only a short-lived token,
which is how real GateHub authenticates them.

### Disable Authentication

```bash
MOCKGATEHUB_ENFORCE_AUTHENTICATION=false
```

> **Warning**: Only use this in development. Always enable authentication for realistic testing.

## Development

### Prerequisites

- Go 1.24+
- Docker & Docker Compose
- Redis (required for webhook queue, optional for app storage)

### Building

```bash
go build -o mockgatehub ./cmd/mockgatehub
```

### Running Locally

```bash
# No dependencies. Storage is in-memory and webhook delivery is disabled.
./mockgatehub

# With Redis, so webhooks are delivered
MOCKGATEHUB_REDIS_URL=redis://localhost:6379 ./mockgatehub

# Disable auth for quick iteration
MOCKGATEHUB_ENFORCE_AUTHENTICATION=false ./mockgatehub
```

Then open <http://localhost:8080/ui>.

### Testing

```bash
# All tests (unit + E2E)
make test

# Unit tests only
make unit-tests

# BDD E2E tests (requires Docker)
make e2e-tests

# Coverage report
make coverage
```

## Architecture

```
cmd/mockgatehub/          # Application entry point and route setup
internal/
  ├── auth/               # HMAC signature validation + HTTP middleware
  ├── config/             # Environment variable configuration
  ├── consts/             # Constants (currencies, vault IDs, rates, statuses)
  ├── handler/            # HTTP handlers (auth, identity, core, cards, fees, rates)
  ├── logger/             # Zap structured logging
  ├── models/             # Domain models and API DTOs
  ├── storage/            # Storage layer (interface + memory/Redis implementations)
  ├── utils/              # Utilities (UUID, address generation)
  └── webhook/            # Redis-backed webhook queue, worker, and delivery
features/                 # Gherkin BDD feature files
test/integration/         # Go integration tests
testenv/                  # Godog E2E test runner with Docker Compose
web/                      # Static HTML (KYC iframe, deposit/withdraw iframe)
docs/                     # GateHub API reference documentation
```

## Limitations

- **Sandbox Only**: Designed for development, not production use
- **Redis for webhooks**: Webhook delivery needs Redis. Without it everything
  else still works, but no webhooks are sent.
- **PIN change is unverified**: `POST /cards/v1/token/pin/data` is implemented
  symmetrically with the read path, but no known consumer exercises it yet, so
  the contract is inferred rather than confirmed.
- **Withdrawal rejection payload**: the shape of
  `more-bridge.withdrawal.rejected` has not been confirmed against real GateHub.
- **Endpoints not yet served**: some calls real consumers make are still
  unrouted — `GET /auth/v1/users/organization/{orgID}` (only `PATCH` exists),
  `PUT /auth/v1/users/managed`,
  `PUT /core/v1/transactions/{uuid}/serviceStatus`,
  `POST /core/v1/gateways/{uuid}/transactions`, and
  `POST /core/v1/users/{orgUuid}/accounts`.

## Troubleshooting

### Container won't start

```bash
docker compose logs mockgatehub
```

### Check Redis connection

```bash
redis-cli -n 1 KEYS "balance:*"
```

### Test health endpoint

```bash
curl http://localhost:8080/health
```

### View webhook delivery logs

```bash
docker compose logs <your-app-service> | grep webhook
```

## Contributing

See [AGENTS.md](AGENTS.md) for AI agent development guidelines.

## License

Maintained by the Interledger Foundation.
