# GateHub Cards API - Full Implementation Specification

**Document Version:** 1.0  
**Date:** January 21, 2026  
**Purpose:** Specification for implementing full GateHub Cards API support in MockGateHub

---

## Executive Summary

This document outlines the complete GateHub Cards API as reverse-engineered from the Interledger TestNet wallet application codebase. The Cards API enables virtual and physical debit card issuance, management, and transaction processing integrated with GateHub's multi-currency vault system.

### Current State
MockGateHub currently has **stub implementations** of 4 basic card endpoints that return hardcoded responses without any state management.

### Target State
Full implementation supporting:
- Customer onboarding with KYC integration
- Card lifecycle management (create, activate, lock, unlock, block, close)
- Card transaction processing and webhooks
- PIN management (view, change)
- Card limits (daily, monthly, per-transaction, ATM, e-commerce)
- Card details tokenization and secure retrieval
- Transaction history and pagination

---

## Architecture Overview

### Card System Components

```
┌─────────────────────────────────────────────────────────────┐
│                    GateHub Cards System                       │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │   Customer   │───▶│   Account    │───▶│     Card     │  │
│  │   (Citizen/  │    │   (EUR only) │    │   (Virtual/  │  │
│  │  LegalEntity)│    │              │    │   Physical)  │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
│         │                    │                    │          │
│         │                    │                    │          │
│         ▼                    ▼                    ▼          │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │  Addresses & │    │   Balances   │    │ Transactions │  │
│  │Communication │    │  (Multi-Curr)│    │   & Limits   │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Integration Points

1. **Identity/KYC System**: Customer creation requires verified user (KYC state = `accepted`)
2. **Vault System**: Card accounts are backed by EUR vaults
3. **Wallet System**: Cards are linked to specific wallet addresses (EUR only)
4. **Webhook System**: Card transactions trigger real-time webhooks

---

## API Endpoints Specification

### Base Path
All card endpoints are under `/cards/v1/`

### Authentication Headers
All card requests require:
- `x-gatehub-managed-user-uuid`: The managed user's UUID
- `x-gatehub-app-id`: The card application ID (e.g., `GATEHUB_CARD_APP_ID`)
- Optional HMAC signature headers for production environments

---

## 1. Customer Management

### 1.1 Create Managed Customer

**Endpoint:** `POST /cards/v1/customers/managed`

**Purpose:** Create a card customer (Citizen or LegalEntity) with an associated EUR account and optional initial virtual card.

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
Content-Type: application/json
```

**Request Body:**
```json
{
  "walletAddress": "https://ilp.link/user123",
  "nameOnCard": "John Doe",
  "account": {
    "productCode": "PROD_EUR_ACCOUNT",
    "currency": "EUR",
    "card": {
      "productCode": "PROD_VIRTUAL_CARD"
    }
  },
  "citizen": {
    "name": "John",
    "surname": "Doe",
    "birthPlace": "London"
  }
}
```

**Request Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `walletAddress` | string | Yes | User's wallet address/URL |
| `nameOnCard` | string | Yes | Name to appear on card (max 26 chars) |
| `account.productCode` | string | Yes | Account product code |
| `account.currency` | string | Yes | Must be "EUR" (only supported currency) |
| `account.card.productCode` | string | Yes | Card product code |
| `citizen.name` | string | Yes* | First name (*if type=Citizen) |
| `citizen.surname` | string | Yes* | Last name (*if type=Citizen) |
| `citizen.birthPlace` | string | No | Place of birth |
| `legalEntity` | object | Yes* | For type=LegalEntity customers |

**Response (201 Created):**
```json
{
  "walletAddress": "https://ilp.link/user123",
  "customers": {
    "id": "cust_uuid_123",
    "code": "CUST001",
    "type": "Citizen",
    "citizen": {
      "name": "John",
      "surname": "Doe",
      "birthPlace": "London"
    },
    "accounts": [
      {
        "id": "acc_uuid_456",
        "productCode": "PROD_EUR_ACCOUNT",
        "currency": "EUR",
        "status": "ACTIVE",
        "cards": [
          {
            "id": "card_uuid_789",
            "status": "active",
            "type": "virtual",
            "walletAddress": {
              "id": "wa_uuid_101",
              "url": "https://ilp.link/user123",
              "publicName": "John Doe",
              "active": true
            }
          }
        ]
      }
    ]
  }
}
```

**Implementation Notes:**
- Customer ID must be stored in user's session/profile (`customerId`)
- Customer creation should validate that user KYC is complete (`accepted` state)
- Name on card is limited to 26 characters
- Only EUR currency is currently supported
- Initial card is created automatically with status `active`

---

### 1.2 Get Cards by Customer

**Endpoint:** `GET /cards/v1/customers/{customerId}/cards`

**Purpose:** Retrieve all cards associated with a customer.

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
```

**Response (200 OK):**
```json
[
  {
    "id": "card_uuid_789",
    "status": "active",
    "type": "virtual",
    "walletAddress": {
      "id": "wa_uuid_101",
      "url": "https://ilp.link/user123",
      "publicName": "John Doe",
      "active": true
    }
  }
]
```

**Card Status Values:**
- `active`: Card is operational
- `locked`: Temporarily locked by user or issuer
- `blocked`: Permanently blocked
- `SoftDelete`: Card has been closed/deleted

**Implementation Notes:**
- Return active card first (filter out `SoftDelete` status)
- Wallet application adds `isPinSet` and `walletAddress` fields to response

---

## 2. Card Creation (Legacy/Deprecated)

### 2.1 Create Card (Deprecated)

**Endpoint:** `POST /cards/v1/cards/{accountId}/card`

**Purpose:** Create an additional card for an existing account. Now deprecated in favor of creating cards during customer creation.

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
```

**Request Body:**
```json
{
  "nameOnCard": "John Doe",
  "deliveryAddressId": "addr_uuid_123",
  "walletAddress": "https://ilp.link/user123",
  "currency": "EUR",
  "productCode": "PROD_VIRTUAL_CARD",
  "card": {
    "productCode": "PROD_VIRTUAL_CARD"
  }
}
```

**Response (200 OK):**
```json
{
  "id": "card_uuid_new",
  "status": "active",
  "type": "virtual",
  "walletAddress": {
    "id": "wa_uuid_101",
    "url": "https://ilp.link/user123",
    "publicName": "John Doe",
    "active": true
  }
}
```

---

## 3. Card Details & Security

### 3.1 Get Card Details (Tokenized)

**Endpoint:** `POST /cards/v1/token/card-data`

**Purpose:** Obtain a short-lived token to retrieve sensitive card details (PAN, CVV, expiry) via external secure endpoint.

**Security Flow:**
1. Client requests token with public key
2. MockGateHub returns token and external URL
3. Client calls external URL with token to get encrypted card data
4. Client decrypts using private key

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
```

**Request Body:**
```json
{
  "cardId": "card_uuid_789",
  "publicKey": "base64_encoded_rsa_public_key"
}
```

**Response (200 OK):**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "links": [
    {
      "href": "https://secure-card-service.example.com/v1/card-data",
      "rel": "card-data",
      "method": "GET"
    }
  ]
}
```

**External URL Response (using token):**
```json
{
  "cipher": "encrypted_card_data_base64"
}
```

**Decrypted Cipher Content (example):**
```json
{
  "pan": "5123456789012346",
  "cvv": "123",
  "expiryMonth": "12",
  "expiryYear": "2028"
}
```

**Implementation Notes:**
- Token expires quickly (typically 60 seconds)
- Cipher is RSA-encrypted with client's public key
- For MockGateHub: can simulate encryption or return mock encrypted data

---

### 3.2 Get PIN (Tokenized)

**Endpoint:** `POST /cards/v1/token/pin`

**Purpose:** Obtain token to retrieve encrypted PIN.

**Request/Response:** Similar structure to Get Card Details

**Request Body:**
```json
{
  "cardId": "card_uuid_789",
  "publicKey": "base64_encoded_rsa_public_key"
}
```

**External URL Response:**
```json
{
  "cipher": "encrypted_pin_base64"
}
```

**Decrypted PIN:**
```json
{
  "pin": "1234"
}
```

---

### 3.3 Get Token for PIN Change

**Endpoint:** `POST /cards/v1/token/pin-change`

**Purpose:** Get token to submit new PIN to external secure endpoint.

**Request Body:**
```json
{
  "cardId": "card_uuid_789"
}
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

---

### 3.4 Change PIN

**Endpoint:** `POST /cards/v1/pin/change`

**Purpose:** Submit new encrypted PIN to external secure endpoint.

**Request Headers:**
```
Authorization: Bearer {token-from-get-token-endpoint}
```

**Request Body:**
```json
{
  "cypher": "encrypted_new_pin_base64"
}
```

**Response (204 No Content)**

**Implementation Notes:**
- PIN must be 4 digits
- User's `isPinSet` flag should be updated to `true` after successful change
- Requires user password validation before getting token

---

## 4. Card State Management

### 4.1 Lock Card (Temporary)

**Endpoint:** `PUT /cards/v1/cards/{cardId}/lock?reasonCode={reasonCode}`

**Purpose:** Temporarily lock a card (user can unlock later).

**Query Parameters:**
- `reasonCode`: One of `LockReasonCode` enum values

**Lock Reason Codes:**
- `ClientRequestedLock`: User requested lock
- `LostCard`: Card is lost (can be unlocked if found)
- `StolenCard`: Card was stolen
- `IssuerRequestGeneral`: Issuer requested lock
- `IssuerRequestFraud`: Fraud suspicion
- `IssuerRequestLegal`: Legal hold

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
```

**Request Body:**
```json
{
  "note": "Lost card temporarily, may find it"
}
```

**Response (200 OK):**
```json
{
  "id": "card_uuid_789",
  "status": "locked",
  "type": "virtual",
  "walletAddress": {
    "id": "wa_uuid_101",
    "url": "https://ilp.link/user123",
    "publicName": "John Doe",
    "active": true
  }
}
```

---

### 4.2 Unlock Card

**Endpoint:** `PUT /cards/v1/cards/{cardId}/unlock`

**Purpose:** Unlock a previously locked card.

**Request Body:**
```json
{
  "note": "Found the card"
}
```

**Response (200 OK):**
```json
{
  "id": "card_uuid_789",
  "status": "active",
  "type": "virtual",
  "walletAddress": {
    "id": "wa_uuid_101",
    "url": "https://ilp.link/user123",
    "publicName": "John Doe",
    "active": true
  }
}
```

---

### 4.3 Permanently Block Card (Legacy)

**Endpoint:** `PUT /v1/cards/{cardId}/block?reasonCode={reasonCode}`

**Purpose:** Permanently block a card (cannot be undone).

**Block Reason Codes:**
- `LostCard`
- `StolenCard`
- `IssuerRequestGeneral`
- `IssuerRequestFraud`
- `IssuerRequestLegal`
- `IssuerRequestIncorrectOpening`
- `CardDamagedOrNotWorking`
- `UserRequest`
- `IssuerRequestCustomerDeceased`
- `ProductDoesNotRenew`

**Response:**
```json
{
  "id": "card_uuid_789",
  "status": "blocked"
}
```

---

### 4.4 Close Card

**Endpoint:** `DELETE /cards/v1/cards/{cardId}/card?reasonCode={reasonCode}`

**Purpose:** Soft-delete a card (status becomes `SoftDelete`).

**Query Parameters:**
- `reasonCode`: Currently only `UserRequest` supported

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
```

**Response (200 OK)**

**Implementation Notes:**
- Requires password validation before closing
- Sets card status to `SoftDelete`
- Card will no longer appear in active cards list

---

## 5. Card Transactions

### 5.1 Get Card Transactions

**Endpoint:** `GET /cards/v1/cards/{cardId}/transactions?pageSize={size}&pageNumber={page}`

**Purpose:** Retrieve paginated transaction history for a card.

**Query Parameters:**
- `pageSize`: Number of transactions per page (default: 20)
- `pageNumber`: Page number (1-indexed)

**Request Headers:**
```
x-gatehub-managed-user-uuid: {user-uuid}
x-gatehub-app-id: {card-app-id}
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "id": 12345,
      "transactionId": "tx_abc123",
      "type": 0,
      "txStatus": "completed",
      "transactionAmount": "50.00",
      "transactionCurrency": "EUR",
      "billingAmount": "50.00",
      "billingCurrency": "EUR",
      "merchantName": "Coffee Shop",
      "transactionDateTime": "2026-01-21T10:30:00Z",
      "processDateTime": "2026-01-21T10:30:05Z",
      "cardScheme": 2,
      "ghResponseCode": "00",
      "responseCode": "00",
      "cardId": 789,
      "vaultId": 1,
      "wallet": 101
    }
  ],
  "pagination": {
    "pageNumber": 1,
    "pageSize": 20,
    "totalPages": 5,
    "totalRecords": 87
  }
}
```

**Transaction Types (CardTrxTypeEnum):**
- `0`: Purchase
- `1`: ATM Withdrawal
- `6`: Card Verification Inquiry
- `17`: Cash Advance
- `20`: Refund/Credit Payment
- `30`: Balance Inquiry on ATM
- `91`: PIN Unblock
- `92`: PIN Change
- `101`: Preauthorization
- `102`: Preauthorization Incremental
- `103`: Preauthorization Completion
- `107`: Transfer to Account
- `108`: Transfer from Account

**Transaction Statuses:**
- `pending`: Authorization hold
- `completed`: Settled transaction
- `declined`: Rejected by issuer
- `reversed`: Transaction reversed/refunded

**Implementation Notes:**
- Transactions with `transactionClassification: 'Advice'` are informational only
- `billingAmount` may differ from `transactionAmount` due to currency conversion
- `mastercardConversion.convRate` contains conversion rate if applicable

---

## 6. Card Limits

### 6.1 Get Card Limits

**Endpoint:** `GET /cards/v1/cards/{cardId}/limits`

**Purpose:** Retrieve all spending limits configured for a card.

**Response (200 OK):**
```json
[
  {
    "type": "dailyOverall",
    "limit": 1000.00,
    "currency": "EUR",
    "isDisabled": false
  },
  {
    "type": "perTransaction",
    "limit": 500.00,
    "currency": "EUR",
    "isDisabled": false
  },
  {
    "type": "monthlyOverall",
    "limit": 5000.00,
    "currency": "EUR",
    "isDisabled": false
  },
  {
    "type": "dailyAtm",
    "limit": 300.00,
    "currency": "EUR",
    "isDisabled": false
  },
  {
    "type": "dailyEcomm",
    "limit": 800.00,
    "currency": "EUR",
    "isDisabled": false
  }
]
```

**Limit Types (CardLimitType):**
- `perTransaction`: Maximum single transaction amount
- `dailyOverall`: Total daily spending limit
- `weeklyOverall`: Total weekly spending limit
- `monthlyOverall`: Total monthly spending limit
- `dailyAtm`: Daily ATM withdrawal limit
- `dailyEcomm`: Daily e-commerce spending limit
- `monthlyOpenScheme`: Monthly open scheme limit
- `nonEUPayments`: Non-EU payment limit

---

### 6.2 Create or Override Card Limits

**Endpoint:** `PUT /cards/v1/cards/{cardId}/limits`

**Purpose:** Set or update card spending limits.

**Request Body:**
```json
[
  {
    "type": "dailyOverall",
    "limit": "1500.00",
    "currency": "EUR",
    "isDisabled": false
  },
  {
    "type": "perTransaction",
    "limit": "750.00",
    "currency": "EUR",
    "isDisabled": false
  }
]
```

**Response (201 Created):**
```json
[
  {
    "type": "dailyOverall",
    "limit": 1500.00,
    "currency": "EUR",
    "isDisabled": false
  },
  {
    "type": "perTransaction",
    "limit": 750.00,
    "currency": "EUR",
    "isDisabled": false
  }
]
```

**Implementation Notes:**
- Limits are enforced during transaction authorization
- `isDisabled: true` means the limit is not enforced
- Only provided limits are updated; others remain unchanged

---

## 7. Webhooks

### 7.1 Card Transaction Authorization

**Event Type:** `cards.transaction.authorization`

**Purpose:** Sent when a card transaction requires authorization (real-time notification).

**Webhook Payload:**
```json
{
  "uuid": "webhook_uuid_123",
  "timestamp": "2026-01-21T10:30:00Z",
  "event_type": "cards.transaction.authorization",
  "user_uuid": "user_uuid_456",
  "environment": "sandbox",
  "data": {
    "authorizationData": {
      "id": 12345,
      "transactionId": "tx_abc123",
      "type": 0,
      "txStatus": "pending",
      "transactionAmount": "50.00",
      "transactionCurrency": "EUR",
      "billingAmount": "50.00",
      "billingCurrency": "EUR",
      "merchantName": "Coffee Shop",
      "transactionDateTime": "2026-01-21T10:30:00Z",
      "cardScheme": 2,
      "ghResponseCode": "00",
      "cardId": 789,
      "vaultId": 1
    },
    "message": "Card transaction authorization"
  }
}
```

**Processing Flow:**

1. **Authorization Request:** Card transaction initiated at merchant
2. **Balance Check:** Verify user has sufficient EUR balance in vault
3. **Limit Check:** Ensure transaction doesn't exceed card limits
4. **Hold Balance:** Reserve transaction amount (create pending transaction)
5. **Send Webhook:** Notify wallet backend of authorization
6. **Settlement:** Transaction moves from pending to completed when settled
7. **Release or Capture:** Either release hold or finalize charge

**Transaction States:**
- **Pending (Authorization):** Amount is held, not yet charged
- **Completed (Settlement):** Transaction finalized, balance deducted
- **Declined:** Transaction rejected (insufficient funds, limit exceeded)
- **Reversed:** Authorization cancelled or refunded

**Implementation Considerations:**
- Webhook must be sent in real-time (< 1 second)
- Failed authorizations should still send webhook with declined status
- Balance holds should expire after 7 days if not settled
- Multiple authorization increments possible (e.g., hotel pre-auth)

---

## 8. Card Products (Metadata)

### 8.1 Get Card Application Products (Deprecated)

**Endpoint:** `GET /v1/card-applications/{cardAppId}/card-products`

**Purpose:** Retrieve available card product codes and their default limits. Now deprecated as product codes are pre-configured.

**Response:**
```json
[
  {
    "uuid": "prod_uuid_virtual",
    "accountProductCode": "PROD_EUR_ACCOUNT",
    "code": "PROD_VIRTUAL_CARD",
    "name": "Virtual Debit Card",
    "cost": "0.00",
    "deletedAt": null,
    "cardProductLimits": [
      {
        "type": "dailyOverall",
        "currency": "EUR",
        "limit": "1000.00",
        "isDisabled": false
      }
    ]
  }
]
```

---

### 8.2 Order Plastic Card (Deprecated)

**Endpoint:** `POST /cards/v1/cards/{cardId}/plastic`

**Purpose:** Order physical card to be shipped to user's address. Deprecated as physical cards are no longer issued through this endpoint.

---

## 9. Data Model

### Customer
```typescript
{
  id: string                    // UUID
  code: string                  // Customer code (e.g., "CUST001")
  type: 'Citizen' | 'LegalEntity'
  sourceId?: string             // External reference
  taxNumber?: string            // Tax identification
  citizen?: {
    name: string
    surname: string
    birthDate?: string
    birthPlace?: string
    gender?: 'Female' | 'Male' | 'Unspecified' | 'Unknown'
    title?: string
    language?: string
  }
  legalEntity?: {
    longName: string
    shortName: string
    sector?: string
    vat?: string
  }
  addresses?: Address[]
  communications?: Communication[]
  accounts?: Account[]
}
```

### Account
```typescript
{
  id: string                    // UUID
  customerId: string
  sourceId?: string
  type?: 'CHARGE' | 'LOAN' | 'DEBIT' | 'PREPAID'
  productCode: string
  accountNumber?: string
  currency: 'EUR'
  status: 'ACTIVE' | 'LOCKED' | 'BLOCKED'
  statusReasonCode?: string
  cards?: Card[]
}
```

### Card
```typescript
{
  id: string                    // UUID
  accountId: string
  status: 'active' | 'locked' | 'blocked' | 'SoftDelete'
  type: 'virtual' | 'physical'
  last4?: string                // Last 4 digits of PAN
  nameOnCard: string
  walletAddress: {
    id: string
    url: string
    publicName: string
    active: boolean
  }
  // Internal fields (not returned by API)
  pan?: string                  // Full card number (PCI)
  cvv?: string                  // CVV (PCI)
  expiryMonth?: string
  expiryYear?: string
  pin?: string                  // Encrypted PIN
}
```

### Card Transaction
```typescript
{
  id: number                    // Internal ID
  transactionId: string         // External reference
  cardId: number
  userId: string
  vaultId: number
  type: CardTrxTypeEnum
  txStatus: 'pending' | 'completed' | 'declined' | 'reversed'
  transactionAmount: string
  transactionCurrency: string
  billingAmount: string
  billingCurrency: string
  merchantName?: string
  terminalId?: string
  transactionDateTime: string
  processDateTime?: string
  cardScheme: number            // 1=Visa, 2=Mastercard
  ghResponseCode: string        // "00"=approved
  responseCode?: string
  mastercardConversion?: {
    convRate?: string
  }
  transactionClassification?: 'Advice'
}
```

---

## 10. Implementation Roadmap for MockGateHub

### Phase 1: Core Card Management (Essential)
**Priority: HIGH**

- [ ] **Customer Management**
  - [ ] Create managed customer endpoint (full implementation)
  - [ ] Get cards by customer endpoint
  - [ ] Store customer data in storage layer (memory/Redis)
  - [ ] Validate KYC status before customer creation
  
- [ ] **Card Lifecycle**
  - [ ] Create card (deprecated endpoint, minimal support)
  - [ ] Get card details (non-tokenized version for testing)
  - [ ] Lock/Unlock card with state management
  - [ ] Close card (soft delete)
  - [ ] Card status transitions validation

- [ ] **Data Models**
  - [ ] Create Customer, Account, Card models
  - [ ] Storage interface updates
  - [ ] Migration from stub responses to real data

### Phase 2: Transactions (Core Functionality)
**Priority: HIGH**

- [ ] **Transaction Processing**
  - [ ] Create card transaction (authorization)
  - [ ] Balance hold mechanism
  - [ ] Transaction settlement
  - [ ] Get card transactions (with pagination)
  - [ ] Transaction status transitions
  
- [ ] **Balance Integration**
  - [ ] Link cards to EUR vaults
  - [ ] Reserve balance on authorization
  - [ ] Deduct balance on settlement
  - [ ] Limit checks during authorization

- [ ] **Webhooks**
  - [ ] Card transaction authorization webhook
  - [ ] Webhook retry logic for card events

### Phase 3: Limits & Security (Important)
**Priority: MEDIUM**

- [ ] **Card Limits**
  - [ ] Get card limits
  - [ ] Create/override card limits
  - [ ] Limit enforcement during transactions
  - [ ] Daily/weekly/monthly rolling window calculations
  
- [ ] **PIN Management**
  - [ ] Get PIN (tokenized/mock)
  - [ ] Change PIN endpoint
  - [ ] PIN validation
  - [ ] Track `isPinSet` flag

### Phase 4: Advanced Features (Nice-to-Have)
**Priority: LOW**

- [ ] **Tokenization Simulation**
  - [ ] Mock RSA encryption for card details
  - [ ] Token generation with expiry
  - [ ] External endpoint simulation
  
- [ ] **Physical Cards**
  - [ ] Order plastic card endpoint (stub)
  - [ ] Delivery tracking (stub)
  
- [ ] **Advanced Transaction Types**
  - [ ] Preauthorization flow
  - [ ] Preauthorization completion
  - [ ] Refunds and reversals
  - [ ] ATM withdrawals

### Phase 5: Testing & Documentation
**Priority: HIGH (after Phase 1-2)

- [ ] **Unit Tests**
  - [ ] Customer creation tests
  - [ ] Card state transition tests
  - [ ] Transaction processing tests
  - [ ] Limit enforcement tests
  
- [ ] **Integration Tests**
  - [ ] Full card lifecycle test (testenv)
  - [ ] Transaction webhook flow test
  - [ ] Multi-card scenarios
  
- [ ] **Documentation**
  - [ ] Update README with card endpoints
  - [ ] Update AGENTS.md with card patterns
  - [ ] Add card examples to integration tests

---

## 11. Storage Layer Extensions

### New Storage Interface Methods

```go
// Customer operations
CreateCustomer(customer *models.Customer) error
GetCustomer(id string) (*models.Customer, error)
GetCustomerByUser(userID string) (*models.Customer, error)
UpdateCustomer(customer *models.Customer) error

// Account operations
CreateAccount(account *models.Account) error
GetAccount(id string) (*models.Account, error)
GetAccountsByCustomer(customerID string) ([]*models.Account, error)

// Card operations
CreateCard(card *models.Card) error
GetCard(id string) (*models.Card, error)
GetCardsByCustomer(customerID string) ([]*models.Card, error)
GetCardsByAccount(accountID string) ([]*models.Card, error)
UpdateCard(card *models.Card) error

// Card transaction operations
CreateCardTransaction(tx *models.CardTransaction) error
GetCardTransaction(id string) (*models.CardTransaction, error)
GetCardTransactions(cardID string, page, pageSize int) ([]*models.CardTransaction, int, error)
UpdateCardTransaction(tx *models.CardTransaction) error

// Card limit operations
SetCardLimits(cardID string, limits []*models.CardLimit) error
GetCardLimits(cardID string) ([]*models.CardLimit, error)
GetCardLimitUsage(cardID string, limitType string, timeWindow string) (float64, error)
```

---

## 12. Key Implementation Challenges

### 12.1 Balance Management
**Challenge:** Card transactions need to hold and release balances atomically.

**Solution:**
- Use Redis transactions (WATCH/MULTI/EXEC) for atomic balance operations
- Track pending holds separately from settled transactions
- Implement hold expiration (7 days)

### 12.2 Limit Enforcement
**Challenge:** Multiple limit types with rolling time windows.

**Solution:**
- Store transactions with timestamps
- Calculate rolling windows on-demand for each limit check
- Cache recent calculations to improve performance

### 12.3 Transaction State Machine
**Challenge:** Complex state transitions (pending → completed/declined/reversed).

**Solution:**
```
         ┌──────────────┐
         │ Authorization│
         └──────┬───────┘
                │
         ┌──────▼───────┐
         │   Pending    │
         └──┬────┬────┬─┘
            │    │    │
    ┌───────▼─┐ │ ┌──▼────────┐
    │Completed│ │ │  Reversed │
    └─────────┘ │ └───────────┘
                │
           ┌────▼────┐
           │Declined │
           └─────────┘
```

### 12.4 Real-time Webhooks
**Challenge:** Authorization webhooks must be near-instant.

**Solution:**
- Use goroutines for async webhook delivery
- Implement webhook queue with retry
- Monitor webhook delivery success rate

### 12.5 EUR-Only Constraint
**Challenge:** Cards only support EUR, but vaults support 11 currencies.

**Solution:**
- Validate currency on account creation
- Auto-select EUR vault for card transactions
- Block non-EUR card creation attempts

---

## 13. Testing Strategy

### 13.1 Unit Test Coverage

```go
// Customer creation
TestCreateCustomer_Success
TestCreateCustomer_KYCNotComplete
TestCreateCustomer_InvalidCurrency
TestCreateCustomer_NameOnCardTooLong

// Card lifecycle
TestLockCard_Success
TestLockCard_AlreadyLocked
TestUnlockCard_Success
TestUnlockCard_NotLocked
TestCloseCard_Success
TestCloseCard_AlreadyClosed

// Transactions
TestAuthorizeTransaction_Success
TestAuthorizeTransaction_InsufficientFunds
TestAuthorizeTransaction_LimitExceeded
TestSettleTransaction_Success
TestReverseTransaction_Success

// Limits
TestSetCardLimits_Success
TestEnforceDailyLimit_Success
TestEnforcePerTransactionLimit_Success
```

### 13.2 Integration Test Scenarios

**Scenario 1: Complete Card Lifecycle**
1. Create verified user (KYC accepted)
2. Create customer with initial card
3. Get cards by customer
4. Create test transaction (purchase)
5. Verify webhook sent
6. Lock card
7. Attempt transaction (should fail)
8. Unlock card
9. Close card
10. Verify card status is SoftDelete

**Scenario 2: Transaction Limits**
1. Create card with EUR 100 daily limit
2. Make EUR 60 purchase (success)
3. Make EUR 50 purchase (should fail - exceeds limit)
4. Make EUR 30 purchase (success - within limit)
5. Verify daily limit tracking

**Scenario 3: Multi-Card Customer**
1. Create customer with initial card
2. Create second card for same account
3. Verify both cards share same balance
4. Transaction on card 1 affects card 2 balance
5. Lock card 1 (card 2 still active)

---

## 14. Security Considerations

### 14.1 PCI Compliance (Simulated)
- Store mock PANs in memory only (not Redis)
- Encrypt sensitive card data at rest
- Use tokenization for card detail retrieval
- Never log full PAN, CVV, or PIN

### 14.2 Authentication
- Validate `x-gatehub-managed-user-uuid` matches card owner
- Enforce `x-gatehub-app-id` for card operations
- Optional HMAC signature validation

### 14.3 Authorization
- User can only access their own cards
- Password required for: view PIN, change PIN, close card
- Validate KYC status before customer creation

---

## 15. Backward Compatibility

### Current Stub Endpoints
The existing stub endpoints must continue to work for any existing tests:

**Preserved Endpoints:**
- `POST /customers/managed` → Now fully implemented
- `POST /cards` → Now fully implemented
- `GET /cards/{cardID}` → Enhanced with real data
- `DELETE /cards/{cardID}` → Enhanced with state management

**Migration Path:**
1. Implement new endpoints alongside stubs
2. Update tests to use new endpoints
3. Deprecate stubs (or keep as fallback)

---

## 16. Open Questions

1. **Physical Cards**: Should MockGateHub support physical card simulation (shipping status, activation)?
2. **Multi-Currency**: Future support for USD/GBP cards, or remain EUR-only?
3. **3D Secure**: Should we simulate 3DS challenge flow for e-commerce transactions?
4. **Chargebacks**: Implement dispute/chargeback simulation?
5. **Card Replacement**: Support lost/stolen card replacement workflow?

---

## 17. Success Metrics

### Implementation Completeness
- [ ] All Phase 1 endpoints implemented
- [ ] 80%+ test coverage for card functionality
- [ ] Integration tests pass
- [ ] Wallet application works without code changes

### Performance Targets
- Card authorization webhook: < 100ms
- Transaction creation: < 50ms
- Get transactions (paginated): < 100ms

### Compatibility
- [ ] TestNet wallet backend works with MockGateHub cards
- [ ] All card API responses match GateHub sandbox format
- [ ] Webhooks trigger correct wallet backend behavior

---

## Appendix A: Complete Endpoint Summary

| Method | Endpoint | Status | Priority |
|--------|----------|--------|----------|
| POST | `/cards/v1/customers/managed` | Stub → Implement | HIGH |
| GET | `/cards/v1/customers/{id}/cards` | Missing | HIGH |
| POST | `/cards/v1/cards/{accountId}/card` | Missing | LOW (deprecated) |
| POST | `/cards/v1/token/card-data` | Missing | MEDIUM |
| POST | `/cards/v1/token/pin` | Missing | MEDIUM |
| POST | `/cards/v1/token/pin-change` | Missing | LOW |
| POST | `/cards/v1/pin/change` | Missing | LOW |
| PUT | `/cards/v1/cards/{id}/lock` | Missing | HIGH |
| PUT | `/cards/v1/cards/{id}/unlock` | Missing | HIGH |
| PUT | `/v1/cards/{id}/block` | Missing | MEDIUM |
| DELETE | `/cards/v1/cards/{id}/card` | Stub → Implement | HIGH |
| GET | `/cards/v1/cards/{id}/transactions` | Missing | HIGH |
| GET | `/cards/v1/cards/{id}/limits` | Missing | MEDIUM |
| PUT | `/cards/v1/cards/{id}/limits` | Missing | MEDIUM |
| GET | `/v1/card-applications/{id}/card-products` | Missing | LOW (deprecated) |
| POST | `/cards/v1/cards/{id}/plastic` | Missing | LOW (deprecated) |

---

## Appendix B: Sample Test Data

### Test Customer
```json
{
  "walletAddress": "https://local.ilp.link/testcard",
  "nameOnCard": "Test Card User",
  "account": {
    "productCode": "PROD_EUR_ACCOUNT",
    "currency": "EUR",
    "card": {
      "productCode": "PROD_VIRTUAL_CARD"
    }
  },
  "citizen": {
    "name": "Test",
    "surname": "User",
    "birthPlace": "TestCity"
  }
}
```

### Test Card
```json
{
  "id": "card_test_001",
  "status": "active",
  "type": "virtual",
  "pan": "5123456789012346",
  "cvv": "123",
  "expiryMonth": "12",
  "expiryYear": "2028",
  "pin": "1234",
  "nameOnCard": "Test Card User"
}
```

### Test Transaction
```json
{
  "transactionId": "tx_purchase_001",
  "type": 0,
  "transactionAmount": "25.50",
  "transactionCurrency": "EUR",
  "merchantName": "Test Coffee Shop",
  "txStatus": "completed"
}
```

---

## Conclusion

Implementing full GateHub Cards API support in MockGateHub will enable:

1. **Complete local development** of card features without external dependencies
2. **Automated testing** of card lifecycle, transactions, and webhooks
3. **Rapid iteration** on card-related wallet features
4. **Cost savings** by eliminating sandbox API usage during development

The phased approach allows incremental delivery, with Phase 1 & 2 providing core functionality sufficient for most development needs.

---

**Next Steps:**
1. Review this specification for accuracy and completeness
2. Prioritize which phases to implement first
3. Create detailed implementation tasks for Phase 1
4. Begin implementation with customer management endpoints

**Document Maintainer:** AI Agent  
**Review Status:** Awaiting user review  
**Last Updated:** January 21, 2026
