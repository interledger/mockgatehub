# GateHub Transaction Fees

## Overview

This document explains how transaction fees flow between GateHub, MockGateHub, and the Interledger Wallet application. It serves as a reference for introducing configurable fee support to MockGateHub.

**Key Takeaway**: GateHub charges fees on deposits and withdrawals. The interledger-app backend correctly processes these fees (subtracting them from deposits, adding them to withdrawals). However, the **frontend currently hardcodes fee display as "0.00"** and tells users _"For a limited time, the Interledger Wallet will absorb all fees"_. In practice, MockGateHub also returns `fee: "0.00"`, so no fees are ever deducted in local development.

## Fee Data Model

### GateHub API Transaction Response

GateHub returns fees as **string** values in its transaction API (`GET /core/v1/transactions/{id}`):

```json
{
  "uuid": "tx-id",
  "amount": "100.00",
  "total_amount": "101.50",
  "fee": "1.50",
  "status": 100,
  "type": 1,
  "vault": { "uuid": "vault-id" },
  "sending_wallet": { ... },
  "receiving_wallet": { ... }
}
```

All monetary fields (`amount`, `total_amount`, `fee`) are **strings with two decimal places** — never numbers.

### Interledger-App Internal Representation

The app converts GateHub's string fee to a scaled integer using `StringToScaledUInt`:

```
"1.50" → 150  (uint64, scale 2)
"0.00" → 0
"23,35" → 2335  (handles comma-separated thousands)
```

Source: `go/backend/providers/gatehub/ops/utils.go`

The fee is stored in the `transactions` table as `provider_fee` (bigint, default 0):

```sql
column "provider_fee" {
    null = false
    type = bigint
    default = 0
}
```

## Fee Flow by Transaction Type

### Deposits

```
User deposits €100 → GateHub charges €1.50 fee → User receives €98.50
```

**Step-by-step flow:**

1. **Webhook arrives** (`core.deposit.completed`) with `amount: "100.00"` and `tx_uuid`
   - The webhook payload does **not** contain fee information
2. **App fetches transaction** via `GET /core/v1/transactions/{tx_uuid}`
   - This is the `GetFeeFromGatehubTrasaction` Temporal activity
   - It reads the `fee` field from the response (e.g., `"1.50"`)
3. **App calculates net amount**: `credited = amount - fee` → `100.00 - 1.50 = 98.50`
4. **App creates transaction record** with full amount and fee stored separately:
   - `Amount = 10000` (€100.00 scaled)
   - `ProviderFee = 150` (€1.50 scaled)
5. **App credits user balance** with net amount only (€98.50)

**Critical**: The fee is **not** read from the webhook. It is fetched separately by calling `GetTransaction` on GateHub. This means MockGateHub's `GET /core/v1/transactions/{id}` response is the authoritative source of fee data.

Source: `go/backend/providers/gatehub/ops/workflows.go` (`CreateGatehubDeposit`)

### Withdrawals

```
User withdraws €50 → GateHub charges €1.00 fee → €51.00 debited from balance
```

**Step-by-step flow:**

1. **App creates withdrawal** on GateHub via the transaction API
2. **App fetches transaction** to get the fee via `GetTransaction`
3. **App reserves balance**: `amount + fee` is reserved from user balance
4. **Transaction record** stores both amount and fee

Source: `go/backend/providers/gatehub/ops/ops.go` (`validateWithdrawal`, `CreateWithdrawal`)

### Inter-Wallet Payments (P2P)

**No GateHub fees.** The fee is hardcoded to zero:

```go
fee := currency.FromFloat64(0, currency.USD)  // hardcoded to zero
```

The gRPC response also hardcodes fees to zero:

```go
fees := currency.FromUInt64(0, p.SenderAmount.Currency)
ret := &pb.Payment{
    FormattedFees: fees.Format(),  // always "0.00"
}
```

Source: `go/backend/payments/ops/ops.go`, `go/backend/grpc/payments.go`

### Card Transactions

Card transactions use `BillingAmount` directly with **no separate fee field**. Any currency conversion markup is embedded in the difference between `TransactionAmount` (merchant currency) and `BillingAmount` (cardholder currency).

Source: `go/backend/providers/gatehub/ops/activity.go` (`CreateGatehubCardTransaction`)

### Cross-Currency (FX)

The database schema has columns for `fx_fee_percentage` and `protection_fee_percentage`, but **cross-currency payments currently return an error** — these fields are never used:

```go
return args, 0, 0, fmt.Errorf("%w cross currency not supported", payments.ErrInternal)
```

Source: `go/backend/payments/ops/ops.go` (`applyFXCreate`)

## Fee Absorption: What It Means

The frontend displays this message on deposit, withdrawal, and payment screens:

> _"For a limited time, the Interledger Wallet will absorb all fees."_

Along with a hardcoded fee display of `0.00`:

```tsx
<span className='text-weak'>Fees</span>
<span className='text-medium'>0.00</span>
```

Source: `typescript/protea/app/routes/deposit/fynbos.tsx`, `withdraw.tsx`, `pay_.$paymentId/Amount.tsx`

**What "absorb" means in practice today:**

| Layer | What happens |
|-------|-------------|
| **GateHub sandbox** | Returns `fee: "0.00"` (sandbox doesn't charge fees) |
| **MockGateHub** | Returns `fee: "0.00"` (hardcoded) |
| **Backend** | Correctly processes fee: `net = amount - fee`. Since fee is 0, user gets full amount |
| **Frontend** | Hardcodes `"0.00"` display, regardless of what the backend reports |

The backend code **does** properly handle non-zero fees — it would deduct them from deposits and add them to withdrawals. The "absorption" is currently cosmetic: in local/sandbox environments fees are zero anyway, and in production the UI simply hides the real fee from the user at the initiation step, though the transaction detail would show the actual fee via `payment.formattedFees` and `transaction.fees`.

**When GateHub starts charging real fees in production**, the backend would correctly deduct them from user balances. Whether the Interledger Foundation is truly "absorbing" those fees (reimbursing users) or simply not showing them is a product/business decision — the code currently does the latter (hides, doesn't reimburse).

## How Transaction Details Display Fees

On **transaction detail** screens, the real fee is shown:

```go
// go/backend/grpc/transactions.go — transformTransaction()
if tx.ProviderFee != nil {
    fees = tx.ProviderFee.Format()  // real fee from GateHub
}

// For deposits: FundsReceived = Amount - ProviderFee
// For withdrawals: FundsReceived = Amount + ProviderFee
```

So once a transaction is completed, the user can see the actual GateHub fee on the transaction detail page, even though the initiation screen showed `0.00`.

## Current MockGateHub Implementation

### Transaction Fee (always zero)

```go
// internal/handler/core.go — CreateTransaction
feeStr := "0.00"            // Mock: no fees in sandbox
totalAmountStr := amountStr  // Total = amount + fees

// internal/handler/handler.go — CompleteTransaction (iframe)
feeStr := "0.00"
totalAmountStr := amountStr
```

### GetTransaction Response

```go
// internal/handler/core.go — GetTransaction
// Returns the stored Transaction struct directly, including the fee field
tx, err := h.store.GetTransaction(txID)
h.sendJSON(w, http.StatusOK, tx)
```

The `models.Transaction` struct:

```go
type Transaction struct {
    ID          string `json:"uuid"`
    Amount      string `json:"amount"`
    TotalAmount string `json:"total_amount"`
    Fee         string `json:"fee"`           // ← the app reads this
    Status      int    `json:"status"`
    // ...
}
```

### Webhook Payload

The deposit webhook includes `total_fees` but the interledger-app **ignores** this field — it fetches the fee separately via `GetTransaction`:

```go
// From handler.go (iframe deposit)
"total_fees": "0"

// From core.go (API deposit) — no total_fees field at all
```

## What Needs to Change for Fee Support

### MockGateHub Changes

To simulate realistic fees, MockGateHub would need to:

1. **Define fee rates** — either as constants in `internal/consts/` or as environment variables for flexibility:
   - Deposit fee (e.g., 1.5% or flat €1.50)
   - Withdrawal fee (e.g., flat €1.00)
   - Possibly per-currency fee rates

2. **Calculate fees in transaction creation** — when creating a deposit/withdrawal transaction:
   ```
   fee = calculateFee(amount, currency, transactionType)
   total_amount = amount + fee  (for deposits: total charged)
   ```

3. **Return fee in GetTransaction** — the `fee` field must be non-zero so the interledger-app can read it:
   ```json
   {
     "fee": "1.50",
     "amount": "100.00",
     "total_amount": "101.50"
   }
   ```

4. **Optionally include in webhook** — though the app ignores `total_fees` in the webhook payload and prefers `GetTransaction`, maintaining consistency is good practice.

### What Does NOT Need to Change

- **Interledger-app backend** — already handles non-zero fees correctly (`net = amount - fee`)
- **Transaction model** — `Fee`, `Amount`, `TotalAmount` fields already exist as strings
- **GetTransaction endpoint** — already returns the full transaction including `fee`

### Missing Response Fields

The interledger-app's `external.Transaction` type expects fields that MockGateHub currently doesn't return:

```go
// Expected by interledger-app but missing from MockGateHub response:
SendingWallet   Wallet `json:"sending_wallet"`
ReceivingWallet Wallet `json:"receiving_wallet"`
Vault           Vault  `json:"vault"`
```

These may need to be added if the app starts using them for fee-related logic or if their absence causes unmarshalling issues.

## Relationship Between `amount`, `total_amount`, and `fee`

The GateHub transaction API uses this convention:

| Field | Deposits | Withdrawals |
|-------|----------|-------------|
| `amount` | Gross deposit amount | Net withdrawal amount |
| `fee` | Fee charged | Fee charged |
| `total_amount` | `amount` (same) | `amount + fee` |

The interledger-app calculates the net user credit as:
- **Deposits**: `credited = amount - fee`
- **Withdrawals**: `debited = amount + fee` (reserved from balance)

## Transaction Status Codes

For reference, the status codes used when the app polls/validates transactions:

| Status | GateHub Value | MockGateHub Value | Match? |
|--------|--------------|-------------------|--------|
| Pending | `0` | `1` | Mismatch (not blocking — app only checks `== 100`) |
| Completed | `100` | `100` | Match |
| Failed | `101` | `3` | Mismatch (not blocking currently) |

## References

- Deposit workflow: `interledger-app/go/backend/providers/gatehub/ops/workflows.go`
- Fee parsing: `interledger-app/go/backend/providers/gatehub/ops/utils.go`
- Fee fetching: `interledger-app/go/backend/providers/gatehub/ops/activity.go` (`GetFeeFromGatehubTrasaction`)
- Transaction display: `interledger-app/go/backend/grpc/transactions.go` (`transformTransaction`)
- Frontend fee display: `interledger-app/typescript/protea/app/routes/deposit/fynbos.tsx`
- MockGateHub transactions: `mockgatehub/internal/handler/core.go`, `handler.go`
- MockGateHub models: `mockgatehub/internal/models/models.go`
