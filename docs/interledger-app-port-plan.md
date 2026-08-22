# Porting interledger-app's mockgatehub features into the standalone mockgatehub

**Status:** complete — all six tranches implemented, verified against the
published image with zero regressions. See §13 for the outcome and §14 for what
the work uncovered.
**Date:** 2026-08-21
**Driver:** upcoming testnet expansion for GateHub cards support
**Fork source:** `interledger-app/go/mock/mockgatehub` (migrated out of this repo at
interledger-app commit `9b027bd5d`, then developed independently for ~15 PRs)

---

## 1. Why this document exists

interledger-app forked mockgatehub into their monorepo and added a meaningful amount of
functionality. The standalone repo also moved on independently (TOTP, 3DS validation,
card ordering, panic recovery). Neither tree is a superset of the other, so this is a
**cherry-pick exercise, not a merge**.

The hard constraint: **testnet must not break.** testnet consumes mockgatehub as a
published container image (`ghcr.io/interledger/mockgatehub:1.13`, see
`testnet/local/mockgatehub.yaml`), so every change here is a public API change for a
downstream consumer we do not control.

The immediate goal is the cards surface, because the next testnet workstream is GateHub
cards. Tranche A is therefore the priority and is specified in the most detail.

---

## 2. The testnet compatibility contract

Everything in this table is load-bearing for testnet today. Each row is a thing the fork
changed that we must **not** copy, or must copy differently.

| # | Contract | Evidence | Fork does | Our decision |
|---|---|---|---|---|
| C1 | Config comes from **env vars** | `testnet/local/mockgatehub.yaml` sets `LOG_LEVEL`, `MOCKGATEHUB_REDIS_URL`, `MOCKGATEHUB_REDIS_DB`, `MOCKGATEHUB_VALID_CREDENTIALS`, `WEBHOOK_URL`, `WEBHOOK_SECRET`, `WEBHOOK_MIN_DELAY_SEC` | Replaced entirely with `CONFIG=<yaml>` via the internal `interledger-app/go/configa` library; `os.Exit(1)` if `CONFIG` unset | **Reject.** Keep env vars. Port only the two new *fields* as env vars. |
| C2 | `POST /cards/v1/token/{tokenType}` wildcard | `client.ts:537` `/token/card-data`, `:632` `/token/pin`, `:673` `/token/pin-change` | Narrowed to a literal `POST /token/card-data` | **Reject the narrowing.** Keep the wildcard; special-case `card-data`. |
| C3 | `POST /cards/v1/cards/{cardID}/limits` | `client.ts:616` `createOrOverrideCardLimits` uses POST | Route deleted | **Reject.** Keep the POST route. |
| C4 | `expiryDate` on the card object | `features/cards.feature` in this repo asserts it | Field removed from `models.Card` | **Reject.** Keep it. |
| C5 | 3DS pending-confirmation shape | this repo's `73cbdd4` "3ds validation and card ordering now working" | Reverts `timeout` to RFC3339 and swaps the confirm response `confirmed` → `status` | **Reject.** Standalone is newer here. |
| C6 | Currency prefix tolerance (`EUR`, `PW_EUR`, `DEB_EUR`) | `handler/cards.go` strips any `*_` prefix | Only strips `PW_`; rejects `DEB_EUR` | **Reject.** Keep the tolerant version. |
| C7 | Withdrawals complete immediately | testnet handles **no** withdrawal webhook — see `service.ts:84-137`, which switches only on `id.verification.*`, `id.document_notice.*`, `cards.transaction.authorization`, `core.deposit.completed` | Withdrawals become `pending` and require an admin trigger | **Adapt.** Port the lifecycle but gate it behind a flag defaulting to today's behaviour. |
| C8 | Card sensitive-data response key is `cypher` | `frontend/src/lib/api/card.ts:107` `{ cypher: string }`; `backend/src/card/service.ts:145` | Returns `cypher` | **Accept as-is.** (Note `backend/src/card/types.ts:12` declares `cipher` — dead/unused mismatch on testnet's side, worth a heads-up to that team.) |
| C9 | `GET /id/v1/users/{id}` response | testnet's `IGetUserStateResponse` reads only `profile` and `verifications` | Renames top-level `id` → `uuid` | **Adapt.** Emit **both** `id` and `uuid`. Free safety. |
| C10 | Detailed 4xx error messages | this repo's handlers | Stripped back to terse strings | **Reject.** Keep the detailed messages; they are the point of a mock. |
| C11 | `/health` returns `"service":"mockgatehub"` | `testnet/local/mockgatehub.yaml` healthcheck | Returns `"service":"github.com/interledger/interledger-app/go/mock/mockgatehub"` — collateral damage from a module-path find/replace | **Reject.** Same applies to the mangled log strings in `cmd/mockgatehub/main.go`. |

> **Note on C1:** the fork's config rewrite is the single biggest blocker to any
> wholesale merge. It is also why we cannot simply vendor the fork's files — every
> handler that reads `h.config` transitively depends on it. Tranche F introduces
> `h.config` on `Handler` with an env-var loader so the rest of the port has somewhere
> to hang.

---

## 3. Tranche A — Cards (priority)

This is the tranche that unblocks the testnet cards work.

### A1. Card transaction scenario catalogue

Port `internal/handler/mockdata/cardtransaction_mocks.json` (17 scenarios). It maps
1:1 onto testnet's `CardTrxTypeEnum` (`shared/src/types/transaction.ts:5-19`), which is
strong evidence it is the right dataset:

| Group | Scenarios |
|---|---|
| Withdrawal / Authorization | Purchase, Purchase with FX, ATM Withdrawal, Cash Advance, Preauthorization, Transfer from Account, Preauth Incremental (with and without `refTransactionId`) |
| Deposit / Authorization | Transfer to Account (Mastercard Send) |
| Deposit / Reversal | Purchase, ATM Withdrawal |
| Declines (`Operation - None`) | Insufficient Balance, Card Verification Inquiry, Incorrect CVV, Wrong PIN, Wrong Card Expiration Date, Locked/Frozen Card |

**Files:** new `internal/handler/mockdata/cardtransaction_mocks.json`, new
`internal/handler/cardscenarios.go` (embed + index + lookup by group/classification/case).

**Fix while porting:** the JSON uses `"MastercardConversion"` (capital M).
testnet's `ICardTransaction` expects `mastercardConversion`
(`shared/src/types/card.ts:55`). Normalise the key on load and add the field to
`models.CardTransaction`.

### A2. Raw card-transaction storage + sequence counter

The catalogue payloads carry fields `models.CardTransaction` does not model. Storing the
raw JSON and returning it verbatim is what makes the catalogue faithful.

- `storage.Storage`: add `StoreRawCardTransaction`, `GetRawCardTransaction`,
  `UpdateCardTransactionStatus`, `NextCardTransactionSeqID`, `PeekCardTransactionSeqID`.
- `MemoryStorage`: `rawCardTransactions map[string]json.RawMessage` + `atomic.Int64` counter.
- `RedisStorage`: new `cardTransactionRawKey` namespace + `INCR` for the counter.
- `GetCardTransaction` handler: prefer the raw payload when present, else the typed struct.
- `models.CardTransaction`: add `ID *int \`json:"id"\`` — testnet's
  `ICardTransaction.id: number` (`shared/src/types/card.ts:35`) is currently **missing
  from the mock entirely**.

### A3. Card transaction simulation API

The fork only exposes simulation through its HTML admin UI. testnet's e2e needs a
programmatic path, so add both:

- `GET  /admin/card-transactions/scenarios` — list catalogue entries (group, classification, case).
- `POST /admin/card-transactions/simulate` — body `{userId, cardId, scenario, overrides?, event?}`.
  Materialises the scenario, assigns `transactionId` + seq `id`, stores raw + typed,
  indexes by card, and fires the webhook.
- `POST /admin/card-transactions/{txID}/status` — drive `txStatus` transitions
  (`PROCESSING` → `COMPLETED`/`REVERSED`).

Register under the existing `auth.PublicEndpointPatterns` test-support convention
(`/admin/card-transactions/*`), matching how `/admin/users/*/fees` is already handled.

### A4. Card transaction webhook — correct event name

**This is the most important correctness fix in the whole port.**

Both trees define `WebhookEventCardTransaction = "cards.transaction.event"` and the
standalone never fires it at all. The fork fires it with a *notification*-shaped payload
`{title, body, transactionId, cardId}` (`ui.go:453-459`).

testnet consumes `cards.transaction.authorization` with
`{authorizationData: ICardTransaction}` (`service.ts:129-134`, `types.ts:215-218`,
`service.ts:195-205` reads `transaction.transactionId`, `.billingCurrency`,
`.billingAmount`). **The fork's payload would be silently dropped by testnet.**

Plan:
- Add `WebhookEventCardTransactionAuthorization = "cards.transaction.authorization"`.
- Default the simulation endpoint to that event with payload
  `{"authorizationData": <full raw tx>, "message": "..."}`.
- Keep `cards.transaction.event` reachable via the `event` field for interledger-app
  parity, but it is not the default.

### A5. `cards.card.created` webhook

`WebhookEventCardCreated` is declared in this repo's consts and **never fired**. Port the
fork's emission from `CreateManagedCustomer` (`cards.go:203-215`): `cardId`,
`cardSourceId`, `nameOnCard`, `productCode`, `maskedPan`, `accountId`, `accountSourceId`,
`lockLevel`, `customerId`, `customerSourceId`.

Drop the fork's `go` in front of `SendAsync` — `SendAsync` is already asynchronous, so
`go h.webhookManager.SendAsync(...)` is a redundant goroutine (the fork does this in
several places).

### A6. Card-data tokenisation + encryption

Port `internal/handler/card_token.go` essentially intact — it is good work: HS256 JWT
carrying `cardId` + the caller's RSA public key, `exp` required, enforced max TTL to stop
the unauthenticated data endpoint becoming an encryption oracle, and PKCS#1 v1.5
encryption of an SPKI key exported by `crypto.subtle`.

**Two deviations from the fork, both required:**

1. **Keep the `{tokenType}` wildcard** (C2). Inside `GetCardToken`, branch:
   `card-data` → JWT path; `pin` / `pin-change` → today's `mock-<type>-<cardID>` token.
2. `links[0].href` becomes absolute via a new `PublicBaseURL` config value. testnet
   currently ignores the href and uses its own `CARD_DATA_HREF` env var
   (`client.ts:554`), so this is additive — but it lets testnet drop that env var later.

New: `GET /cards/v1/token/card-data/data`, HMAC-exempt (add to `auth.PublicEndpoints`),
Bearer-authenticated by the JWT, returns `{"cypher": "<base64 RSA ciphertext>"}` over
`{Pan, ExpiryDate, Cvc2}`. PAN is derived deterministically from `cardID` via SHA-256 so
repeat calls are stable.

### A7. PIN endpoints — a gap in *both* trees

Neither the standalone nor the fork implements what testnet's `CARD_PIN_HREF` points at.
testnet needs:

- `GET  /cards/v1/token/pin/data` — Bearer `pin` token → `{"cypher": ...}` with the
  encrypted PIN. Consumed by `client.ts:648-652` (`getPin`).
- `POST /cards/v1/token/pin/data` — Bearer `pin-change` token, body `{"cypher": ...}`,
  RSA-decrypt and store the new PIN. Consumed by `client.ts:692-699` (`changePin`).

This requires the mock to hold an RSA **key pair** so it can decrypt what the client
encrypts on pin-change (the card-data flow only needs the caller's public key). Generate
the pair at startup and expose the public key — either via a small
`GET /cards/v1/token/pin/public-key` endpoint or a `CARD_PIN_PUBLIC_KEY` config value.

**Open question for review:** confirm with the testnet team how `changePin` obtains the
public key it encrypts against today, so the mock matches. This is the one item in
Tranche A where I do not yet have the real contract.

### A8. Pagination on card transaction listing

`ListCardTransactions` currently hard-codes `pageNumber: 1, pageSize: 20, totalPages: 1`
and ignores the query string. testnet sends `pageSize` and `pageNumber`
(`client.ts:583-597`). Honour them and compute `totalPages` properly.

---

## 4. Tranche B — KYC / identity

Directly useful to testnet: it already handles `id.document_notice.expired` and
`id.document_notice.warning` (`service.ts:120-127`) and the mock cannot currently produce
either.

- **New consts:** `KYCStateResubmission`, `WebhookEventKYCResubmission`,
  `WebhookEventDocumentNoticeExpired`, `WebhookEventDocumentNoticeWarning`.
- **`webhook/manager.go`:** extend the `id.verification.*` enrichment switch to cover the
  three new events; suppress the synthetic `verified` block for them (the fork's
  refinement — real GateHub does not send it for resubmission/document notices).
- **`identity.go` — Sumsub mapping:** replace the binary `status 0|1` with
  `sumsubVerificationStatusState`: accepted → `(1,1)`, rejected → `(2,0)`,
  resubmission → `(10,0)`, else `(0,0)`. Add `provider: "Sumsub"` to the verification object.
- **`identity.go` — selectable KYC outcome:** the iframe submit accepts a `kyc_outcome`
  form value (`accepted` | `rejected` | `action_required`) and fires the matching webhook.
  Defaults to `accepted`, so existing callers are unaffected.
- **`shouldSkipAcceptedWebhook`:** suppress the accepted webhook when the previous state
  was `resubmission`, to avoid racing the app's own transition.
- **`PUT /admin/users/{userID}/kyc-state`** — set state with no webhook, for e2e setup.
  Add `/admin/users/*/kyc-state` to `PublicEndpointPatterns`.
- **`models.User` / `GetUserResponse`:** add `IsProfileCreationDisabled`; emit **both**
  `id` and `uuid` (C9).
- **Signed 2FA callback:** add `X-GH-Webhook-Signature` via
  `auth.GenerateGateHubWebhookSignature` to the TOTP callback POST, replacing the bare
  `httpClient.Post`. Verify against the current `stephan/20260324-signature-fix` work
  before landing — these may overlap.

---

## 5. Tranche C — Withdrawal lifecycle (opt-in)

The fork's model is better (real GateHub withdrawals are asynchronous), but flipping the
default breaks testnet (C7).

- New config `MOCKGATEHUB_ASYNC_WITHDRAWALS` (default **false** = today's behaviour:
  create as completed, deduct immediately, no webhook).
- When true: create as `pending` with `AccountIBAN` / `AccountLegalName` / `Message`
  populated, defer the balance deduction, and expose:
  - `GET  /admin/users/{userID}/withdrawals?status=pending`
  - `POST /admin/withdrawals/{txID}/trigger-event` with
    `{"event": "core.withdrawal.completed" | "more-bridge.withdrawal.rejected"}`
- New consts `WebhookEventWithdrawalCompleted`, `WebhookEventWithdrawalRejected`,
  and `TransactionStatusPending` usage for withdrawals.
- `models.Transaction`: add `AccountIBAN`, `AccountLegalName`, `Message`, `SendingAddress`.
- Add the two `/admin/withdrawals/*` patterns to `PublicEndpointPatterns`.

**Note the fork's own inconsistency**, worth not copying blindly: the completed payload
uses snake_case (`tx_uuid`, `total_fees`) while the rejected payload uses camelCase
(`txUuid`, `userUuid`) and hard-codes `"status": "PENDING"` on a rejection. Confirm the
real GateHub shapes before fixing either way; flag to the interledger-app team regardless.

---

## 6. Tranche D — Statements

Purely additive — testnet makes no statement calls today, so there is zero regression risk.

- New `internal/assets/assets.go` with `//go:embed mock_statement.pdf`. **We need to
  generate our own PDF** rather than copy interledger-app's, which may carry their
  branding. A minimal valid one-page PDF is fine.
- New `internal/handler/statements.go`:
  - `GET /statement/v1/statements/account-confirmation/{walletAddress}`
  - `GET /statement/v1/statements/account-statement/{walletAddress}/{year}/{month}`
    (logs the `networks` and `gateways` query params)
  - `GET /statement/v1/statements/transfer-confirmation/{transactionUUID}` — 404 if the
    transaction is unknown, 400 if it is not a deposit or withdrawal
- All three stay behind HMAC auth (not added to `PublicEndpoints`).
- `Content-Type: application/pdf` + `Content-Disposition: attachment`.

**Improve on the fork:** its three handlers return the same bytes and differ only in
filename. Since we hold the transaction, `transfer-confirmation` should at least log or
echo real transaction values so the mock is diagnosable.

---

## 7. Tranche E — Admin dev UI at `/ui`

619 lines of Go plus four HTML templates. Real developer-experience value: it is how a
human drives KYC transitions, card transactions and withdrawal events without curl.

- New `internal/handler/ui.go` + `internal/handler/web/ui/{dashboard,user,kyc_action,card_tx_action}.html`
  (`//go:embed`, consistent with the existing `web/` templates).
- Routes: `/ui`, `/ui/users/{userID}`, `/ui/actions/kyc` (GET+POST),
  `/ui/actions/card-transaction` (GET+POST) plus `/preview`, `/cards`, `/status`,
  `/ui/actions/withdrawal/status`.
- `auth/middleware.go`: skip auth for `/ui` and `/ui/*`.
- Storage additions: `ListUsers`, `ListTransactionsByUser`, `GetAllBalances`.
- **Rework the card-tx form** to be driven by the A1 catalogue (pick a scenario from a
  dropdown, then edit) rather than the fork's free-text JSON box with a two-field default.
- **Fix while porting:** the fork's webhook preview and the webhook it actually sends are
  built by two separate code paths (`buildUICardTxWebhookPreview` vs the inline map in
  `UICardTxAction`), so the preview can drift from reality. Build one payload and render it.

Do this tranche **last** — it depends on A1–A3 and on the Tranche C storage methods.

---

## 8. Tranche F — Fixes, hygiene, foundations

Small, independent, and Tranche A depends on the first item. Land this first.

1. **`config.Config` on `Handler`.** Add `PublicBaseURL` (`MOCKGATEHUB_PUBLIC_BASE_URL`,
   trailing slash trimmed, default `http://localhost:8080`) and `CardDataTokenSecret`
   (`MOCKGATEHUB_CARD_DATA_TOKEN_SECRET`, random 32 bytes if unset — never a hard-coded
   constant). Add `NewHandlerWithConfig(cfg, store, wm)`; keep `NewHandler` working for
   the existing tests. **Env vars only — no `configa`** (C1).
2. **`sendJSON` status bug** (`helpers.go`): it writes the header before marshalling, so a
   marshal failure emits `200` with an error body. Marshal first, then choose the status.
3. **Unchecked `ParseFloat`** on the deposit and withdrawal iframe paths
   (`handler.go`): a malformed amount silently becomes `0.00`. Return `400`.
4. **`cards.card.created`** — see A5.
5. **Hosted transfer direction** (`core.go`): derive debit vs credit from whether
   `sending_address` resolves to a wallet owned by the user, instead of always crediting.
   From interledger-app PR #463. Covered by two new scenarios in `features/transactions.feature`.
6. **Helpers:** `formatAmount`, `tokenPrefix`, `resolveWalletParam`. `tokenPrefix`
   replaces four hand-rolled clamp sites — `handler.go:132`, `handler.go:180-181`,
   `handler.go:596`, `identity.go:306-307` — and one **unguarded** slice at
   `auth.go:37` (`token[:20]`), which panics on a short token. Today's tokens are always
   long enough, so this is latent rather than live, but it should not survive the port.
7. **Redis-optional in-memory mode** (`main.go`): today in-memory storage still
   `Fatal`s without Redis for the webhook queue. Make the queue optional and log that
   webhooks are disabled. Improves the standalone `go run` story; testnet always sets
   `MOCKGATEHUB_REDIS_URL` so it is unaffected.
8. **`.golangci.yml`** + `golangci-lint run ./...` in the `lint` target. Note the fork
   disables `errcheck` and `staticcheck`; we should try to keep them **on** and fix
   findings, since this repo is smaller and already clean under `go vet`.
9. **Card defaults into consts:** `DefaultCardProductCode`, `DefaultCustomerType`,
   `DefaultAccountType`, `CardRelationPrimary`, `DefaultStatusActive`; extract
   `defaultCardLimits()` (currently duplicated in two places in `cards.go`).
10. **Card transaction type consts** — the standalone has none. Add them from testnet's
    `CardTrxTypeEnum` (Purchase 0, ATMWithdrawal 1, CardVerificationInquiry 6,
    CashAdvance 17, RefundCreditPayment 20, BalanceInquiryOnATM 30, PINUnblock 91,
    PINChange 92, Preauthorization 101, PreauthorizationIncremental 102,
    PreauthorizationCompletion 103, TransferToAccount 107, TransferFromAccount 108).

---

## 9. Explicitly out of scope

| Item | Reason |
|---|---|
| `configa` YAML config | C1 — breaks testnet, dependency unavailable |
| `/token/card-data` route narrowing | C2 — breaks `/token/pin`, `/token/pin-change` |
| `POST /cards/{cardID}/limits` removal | C3 |
| `expiryDate` removal from `models.Card` | C4 |
| 3DS `timeout` RFC3339 + `confirmed` → `status` | C5 — standalone is newer |
| `PW_`-only currency validation | C6 |
| Terse error messages | C10 |
| Module-path find/replace damage in `/health` and log strings | C11 |
| `go` before `SendAsync` | Redundant goroutine; `SendAsync` is already async |

---

## 10. Verification

Baseline confirmed green before starting: `go build ./...` clean and all eight unit test
packages pass (`internal/{auth,config,handler,logger,storage,utils,webhook}` +
`cmd/mockgatehub`; `internal/consts` and `internal/models` have no tests). `testenv` has
no `main` and is godog-only, as expected.

Per tranche:

1. `make lint` (gofmt, `go vet`, golangci-lint).
2. `go test ./internal/... ./cmd/...`.
3. `make e2e-tests` — godog. Port the fork's 7 net-new scenarios (3 KYC outcome, 2 hosted
   transfer, 2 card-data encryption) and add new ones for the card scenario catalogue,
   statements, and the async-withdrawal flag in both positions. Target ~90 scenarios, up
   from 72.
4. **testnet regression gate — the important one.** For each tranche, build the image
   locally, point `testnet/local/mockgatehub.yaml` at it instead of
   `ghcr.io/interledger/mockgatehub:1.13`, and run testnet's own e2e suite
   (`testnet/e2e`). No change ships without this passing.
5. Release as a **minor** version bump (`1.14.0`) — additive only. If Tranche C's flag
   ever flips to default-true, that is a **major**.

---

## 11. Suggested sequencing

| Order | Tranche | Depends on | Rough size |
|---|---|---|---|
| 1 | **F** — fixes + config foundations | — | small |
| 2 | **A** — cards | F1 | large (the priority) |
| 3 | **B** — KYC / identity | F | medium |
| 4 | **D** — statements | F | small |
| 5 | **C** — withdrawal lifecycle (opt-in) | F | medium |
| 6 | **E** — admin UI | A, C | large |

One branch off `main`, one commit per numbered item, build + unit + godog green at each
commit, and the testnet gate run at each tranche boundary.

---

## 12. Open questions for review

1. **A7 / PIN endpoints** — what public key does testnet's `changePin` encrypt against
   today, and where does it get it? This is the only Tranche A contract I could not
   establish from the code.
2. **Tranche C payload shapes** — the fork mixes snake_case and camelCase across the two
   withdrawal webhooks and sends `"status": "PENDING"` on a rejection. Which matches real
   GateHub?
3. **Tranche E scope** — is the admin UI wanted in the standalone at all, or is a
   programmatic-only surface (A3) enough? It is the largest tranche and the least
   load-bearing for testnet.
4. **Divergence going forward** — should we offer these back to interledger-app so the
   two trees reconverge, or accept a permanent fork? Several fixes here (A4's event name,
   E's preview drift, C's payload inconsistency) are bugs on their side.

---

## 13. Outcome

All six tranches landed, one commit each, in the planned order.

| Tranche | Commit | What shipped |
|---|---|---|
| F | `5395f9a` | Config foundations (`PublicBaseURL`, `CardDataTokenSecret`), `sendJSON` status fix, unchecked `ParseFloat`, hosted-transfer direction, `tokenPrefix`, Redis-optional webhook queue, `.golangci.yml` at zero findings |
| A | `6af4594` | 17-scenario card transaction catalogue, raw-payload storage + sequence counter, simulation API, `cards.transaction.authorization`, `cards.card.created`, card-data and PIN encryption, pagination |
| B | `f91a124` | Resubmission state, document notices, Sumsub mapping, selectable KYC outcome, quiet state override, signed 2FA callback |
| D | `0f2179c` | `/statement/v1` with per-request PDF rendering, `internal/pdf`, `ListTransactionsByUser` |
| C | `14559ea` | Opt-in asynchronous withdrawals, settlement endpoints, once-only settlement |
| E | `cbf3903` | Admin UI at `/ui`, `ListUsers`/`GetAllBalances`, shared operation core so UI and API cannot drift |

### Verification

| Gate | Result |
|---|---|
| `gofmt` + `go vet` + `golangci-lint` | 0 findings (errcheck and staticcheck kept on) |
| Unit tests | 10 packages green, all backed by both storage backends where applicable |
| E2E (godog) | **144 scenarios / 1044 steps**, up from 72 scenarios at baseline |
| Mutation checks | Each tranche's headline behaviour was deliberately broken and confirmed to fail the suite — hosted-transfer direction, card webhook payload, resubmission classification, statement content, withdrawal charge timing, UI settlement path |
| testnet regression gate | **0 regressions** across all 28 endpoints testnet calls |

The e2e harness now runs with `Strict: true`, so a scenario whose steps have no
definition fails instead of silently reporting success — worth knowing, because
that is how the pre-existing "user balance increases by" step had come to assert
only an HTTP status.

### How the regression gate was run

testnet's own Playwright suite needs the full stack, and that checkout has no
`node_modules` and no `local/.env`; bringing it up unsupervised would have taken
hours and most likely produced a misleading result. Instead the gate was run
directly against container images:

1. Build this branch as an image; run `ghcr.io/interledger/mockgatehub:1.13`
   (what testnet runs today) alongside it.
2. Drive every one of the 28 endpoints testnet's GateHub client calls against
   both, with valid HMAC signatures, and diff the status codes.
3. Treat `404 → 2xx` as a new capability and any `2xx → 404/5xx` as a regression.

Signing matters: the auth middleware runs before routing, so an unsigned request
returns 401 for a route that does not exist. An unsigned probe cannot tell a
missing endpoint from a rejected one, and initially suggested — wrongly — that
the new routes were already present in 1.13.

The durable version of this gate lives in the repo as
`features/consumer_contract.feature`, which asserts every path, method and
response field testnet depends on. It runs with `make e2e-tests`.

---

## 14. What the work uncovered

### Seven endpoints testnet calls that this service never routed

All seven are identical on `main` — none was introduced by this port. Three are
card features and were fixed here as pure route aliases; four are outside the
cards scope and are left for a decision.

| Endpoint | Status |
|---|---|
| `GET/POST /v1/cards/{cardId}/limits` | **Fixed** — aliased. Card limits could not work against the mock at all. |
| `GET /v1/card-applications/{appId}/card-products` | **Fixed** — aliased. |
| `GET /cards/v1/customers/{customerId}/cards` | **Fixed** — aliased. The card listing call. |
| `GET /auth/v1/users/organization/{orgId}` | Open — only `PATCH` is routed. |
| `PUT /auth/v1/users/managed` | Open — only `POST` and `GET` are routed. |
| `PUT /core/v1/transactions/{uuid}/serviceStatus` | Open — pending-transaction approval. |
| `POST /core/v1/gateways/{uuid}/transactions` | Open — pending-transaction listing. |
| `POST /core/v1/users/{orgUuid}/accounts` | Open — SEPA details. |

`GATEHUB_API_BASE_URL` is a bare host with no path prefix, so the `/v1/...`
paths really did resolve to unrouted URLs rather than being folded under
`/cards/v1`.

### Defects found in interledger-app's fork

Worth passing back to that team; none was copied.

1. **The card transaction webhook cannot be consumed.** The fork emits
   `cards.transaction.event` with a notification payload of
   `{title, body, transactionId, cardId}`. Consumers switch on
   `cards.transaction.authorization` and read `data.authorizationData`, so the
   fork's event is silently dropped by the very consumer it is for.
2. **The FX field never deserializes.** Its fixtures spell the scheme
   conversion object `MastercardConversion`; consumers read
   `mastercardConversion`.
3. **The two withdrawal webhooks disagree with each other.** Completion uses
   snake_case (`tx_uuid`, `total_fees`), rejection uses camelCase (`txUuid`,
   `userUuid`), and the rejection payload reports `"status": "PENDING"` on a
   rejection.
4. **The card-tx preview can drift from what is sent.** The preview payload and
   the delivered payload are built by two separate code paths.
5. **Statements are indistinguishable.** All three endpoints return the same
   bytes, differing only in `Content-Disposition` filename.
6. **The module rename damaged string literals.** `/health` reports
   `"service":"github.com/interledger/interledger-app/go/mock/mockgatehub"`, and
   several log messages carry the module path where the service name belongs.

### Defects found in this repository

Fixed as part of the port.

1. `sendJSON` wrote the status header before marshalling, so an unencodable body
   returned `200` carrying an error payload.
2. `auth.go` sliced `token[:20]` with no length guard — a panic waiting for a
   short token.
3. Deposit and withdrawal amount parsing discarded the `ParseFloat` error.
4. Hosted transfers always credited, whichever way the money moved.
5. `models.CardTransaction` had no `id` field and never populated `cardId`,
   both of which consumers key off.
6. The card transaction listing ignored `pageNumber` and `pageSize` and always
   reported a single page.
7. `WEBHOOK_URL` in the e2e environment pointed at `/test-webhook`, a route that
   did not exist, so every webhook delivery 404'd and no webhook assertion could
   be anything but a no-op.
8. In-memory mode called `logger.Fatal` when Redis was absent, so a plain
   `go run ./cmd/mockgatehub` could not start. (Confirmed while running 1.13,
   which exits immediately without Redis.)
9. Webhook delivery was capped at 10 per 5 seconds; under the fuller suite a
   delivery took 19.8 seconds. This was originally fixed here by making the
   poll interval and batch size configurable. That fix was **dropped during the
   rebase** in favour of the better one that landed on main independently
   (`04f3e7a`): the worker now blocks on a Redis stream and picks a webhook up
   as soon as it is enqueued, so there is no polling interval to tune.

### Answers to the open questions from §12

1. **PIN encryption direction** — resolved from the code. The read direction is
   symmetric with card-data: the consumer sends `publicKeyBase64` and decrypts
   the returned `cypher` with PKCS#1 v1.5. The change direction is not live —
   testnet's change-PIN frontend is entirely commented out — so it is
   implemented the natural symmetric way, with the service publishing its own
   key at `GET /cards/v1/token/pin/public-key`. That half is unverified against
   a live consumer and should be confirmed before anyone relies on it.
2. **Withdrawal payload shapes** — still unconfirmed against real GateHub. Both
   payloads are snake_case here and a rejection reports `REJECTED`. If the real
   API differs, this is a one-line change in `rejectWithdrawal`.
3. **Admin UI scope** — built. It shares the API's code path rather than
   reimplementing it, so its ongoing cost is low.
4. **Reconverging with interledger-app** — still a decision to make. The six
   defects above are all live in their tree.

---

## 15. Rebase onto the updated main

The work was originally based on `stephan/20260324-signature-fix` (`4416c45`),
which was one commit ahead of the `main` visible at the time. Three commits then
landed on main, two of which overlapped this work directly. All seven commits
were rebased onto `origin/main` (`04f3e7a`); the resulting branch is
`5395f9a..e54a3c3`.

| Commit on main | Overlap | Resolution |
|---|---|---|
| `8e97a42` panic recovery (#41) | This is the squashed merge of the branch this work was based on | Absorbed by the rebase; no longer carried separately |
| `6428bed` removed the pending deposit webhook (#42) | Tranche F touched the same block and would have reinstated the webhook | Took main's side — the pending webhook stays removed |
| `04f3e7a` hosted transfers credit and debit (#43) | Implements the same fix as Tranche F, and refactors the webhook queue to Redis Streams | Took main's side throughout; see below |

### What was dropped in favour of main

- **Hosted transfer direction.** Implemented independently on both sides.
  Main's version is canonical and this branch's duplicate was discarded, along
  with its two e2e scenarios, which duplicated main's. One scenario was kept
  because it asserts something main's do not: that the transfer response echoes
  `sending_address` back, which a consumer needs in order to reconcile.
- **Webhook delivery pacing.** Tranche B added `WEBHOOK_POLL_INTERVAL_MS` and
  `WEBHOOK_BATCH_SIZE` to work around a polling worker capped at 2 deliveries a
  second. Main replaced the poller with a Redis Streams consumer group that
  blocks until a job arrives, which addresses the same problem better. The
  config fields, the `NewWorkerWithPacing` constructor, their tests and the
  compose overrides were all removed.
- **The 2-second `WEBHOOK_MIN_DELAY_SEC` clamp.** Removed on main; the value is
  now used as configured.

### One discrepancy worth noting, not changed

Main debits a hosted transfer by `amount - fee`; this branch had debited
`amount + fee`. A sender paying a fee should be debited more than they sent, not
less, so `amount + fee` is arguably the correct reading. It makes no difference
today because hosted transfers carry no fee — `feePercent` is only assigned for
external deposits and withdrawals, so `amount - fee == amount` for every hosted
transfer. Main's version was kept rather than quietly changing behaviour that
has no live effect. If a hosted-transfer fee is ever introduced, this is the
line to revisit.

---

## 16. Splitting the admin surface onto its own port

The admin UI and the test-support endpoints now listen on
`MOCKGATEHUB_ADMIN_PORT` (default `8081`), separate from the application API on
`MOCKGATEHUB_PORT` (default `8080`). The point is to make that surface
guardable: publish only `8080` and it is unreachable from outside.

### Why the whole admin surface moved, not just the UI

Moving only `/ui` would not have achieved anything. The thirteen `/admin/*`
routes are HMAC-exempt by design, and several of them mutate state:

- `POST /admin/withdrawals/{txID}/trigger-event` settles a withdrawal
- `PUT /admin/users/{userID}/kyc-state` changes a user's verification state
- `POST /admin/card-transactions/simulate` creates transactions and emits webhooks
- `PUT /admin/fees` changes fee configuration

A firewall on the UI port alone would have left all of those open on the public
port. `/test-webhook` moved too, so `WEBHOOK_URL` now points at the admin
listener in the test environment.

### The separation is structural

The admin routes are registered on a different router, not merely exempted from
authentication. With `MOCKGATEHUB_ENFORCE_AUTHENTICATION=false` — how some local
setups run — admin paths return **404** on the application port rather than
falling through to a handler. Verified both ways:

| Path | App port (auth on) | App port (auth off) | Admin port |
|---|---|---|---|
| `/ui` | 401 | 404 | 200 |
| `/admin/fees` | 401 | 404 | 200 |
| `/admin/card-transactions/scenarios` | 401 | 404 | 200 |
| `/iframe/onboarding` | 200 | 200 | 404 |
| `/cards/v1/token/pin/public-key` | 200 | 200 | 404 |

The process also refuses to start when both ports are equal, so the surfaces
cannot be silently merged back onto one listener.

The dead auth exemptions were removed rather than left behind: `PublicEndpoints`
no longer lists `/admin/fees`, the `/ui` bypass is gone, and
`PublicEndpointPatterns` is now empty. Keeping them would have widened the
application listener's public surface for paths it no longer serves.

### testnet impact: none

This was investigated before making the change, because a break here would land
on the testnet integration.

| Checked | Result |
|---|---|
| `packages/wallet/backend` — the GateHub client and everything else | No `/admin`, `/test-webhook` or `/ui` reference |
| `e2e/` — features, steps and helpers | Drives MockGatehub only through the deposit iframe and through the wallet backend. No admin usage |
| `local/mockgatehub.yaml` | Publishes `8080:8080` only; traefik routes `mockgatehub.testnet.test` to container port 8080 |
| `local/` scripts and `helm/` | No admin references; helm does not deploy MockGatehub at all |
| Wallet env (`GATEHUB_API_BASE_URL`, the three iframe URLs, `CARD_DATA_HREF`, `CARD_PIN_HREF`) | All resolve to paths that stay on the application port |

So nothing in testnet needs to change for its tests to keep passing. The only
follow-up is **optional**: a developer who wants the admin UI locally should add
`- '8081:8081'` to the `ports` list in `local/mockgatehub.yaml`. That is an
enhancement, not a fix. Anyone deploying MockGatehub should make sure the admin
port is not published to the internet.
