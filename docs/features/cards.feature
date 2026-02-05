Feature: Card management and lifecycle
  As a wallet integrator using cards
  I want to manage card customers and their cards
  So that I can provide card functionality to users

  Background:
    Given a clean MockGatehub instance
    And a managed user with KYC state "accepted"
    And HMAC headers using app id "local-test-app-id" and secret "local-test-app-secret"

  Scenario: Create a managed customer with EUR account and initial card
    When I POST /cards/v1/customers with managed user UUID header, nameOnCard "John Doe", account productCode "PWSR_DEBP_2404", currency "EUR", and card productCode "PWSR_DEBP_2404"
    Then the response status is 201

  Scenario: List cards for a customer
    Given a customer exists with at least one card
    When I GET /cards/v1/cards/mock-customer-id?pageSize=100 with managed user UUID header
    Then the response status is 200

  Scenario: Get card details
    Given a card exists in the system
    When I GET /cards/v1/cards/mock-card-id/card with managed user UUID header
    Then the response status is 200

  Scenario: Lock card temporarily
    Given a card with status "Active"
    When I PUT /cards/v1/cards/mock-card-id/lock?reasonCode=USER_REQUEST with managed user UUID header and note "Testing lock"
    Then the response status is 404

  Scenario: Unlock card
    Given a card with status "TemporaryBlocked"
    When I PUT /cards/v1/cards/mock-card-id/unlock with managed user UUID header and note "Testing unlock"
    Then the response status is 404

  Scenario: Block card permanently
    Given a card with status "Active"
    When I PUT /cards/v1/cards/mock-card-id/block?reasonCode=FRAUD_SUSPECTED with managed user UUID header
    Then the response status is 404

  Scenario: Close/delete card
    Given a card with status "Active"
    When I DELETE /cards/v1/cards/mock-card-id/card?reasonCode=USER_REQUEST with managed user UUID header
    Then the response status is 405

  Scenario: Get card token for secure data access
    Given a card exists with id and panToken
    When I POST /cards/v1/token/card-data
    Then the response status is 200

  Scenario: Get pending 3DS confirmations
    Given at least one card transaction with pending 3DS authentication
    When I GET /cards/v1/transaction/pending-confirmations with managed user UUID header
    Then the response status is 200

  Scenario: Confirm 3DS payment (approve)
    Given a pending 3DS confirmation exists with transactionId
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed true, authMethod "pin"
    Then the response status is 200

  Scenario: Decline 3DS payment
    Given a pending 3DS confirmation exists with transactionId
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed false, authMethod "pin"
    Then the response status is 200

  Scenario: Get card application products
    When I GET /cards/v1/card-applications/test-app-id/card-products with managed user UUID header
    Then the response status is 200

  Scenario: Order plastic card for a card
    Given a card exists with id
    When I POST /cards/v1/cards/mock-card-id/plastic
    Then the response status is 201

  Scenario: Get card limits
    Given a card exists in the system
    When I GET /cards/v1/cards/mock-card-id/limits with managed user UUID header
    Then the response status is 200

  Scenario: Set card limits
    Given a card exists with default limits
    When I PUT /cards/v1/cards/mock-card-id/limits with managed user UUID header
    Then the response status is 400

  Scenario: Create card transaction
    Given a card exists with sufficient limits and balance
    When I POST /cards/v1/transactions
    Then the response status is 400

  Scenario: Get card transaction details
    Given a card transaction exists with transactionId
    When I GET /cards/v1/transactions/mock-transaction-id with managed user UUID header
    Then the response status is 404
