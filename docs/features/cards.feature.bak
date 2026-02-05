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
    And the response contains a customer with id, sourceId, type "Citizen", and kycStatus "accepted"
    And the customer has one account with currency "EUR", status "ACTIVE", and type "PREPAID"
    And the account has one card with status "Active", nameOnCard "John Doe", and expiryDate in the future
    And a cards.card.created webhook is sent

  Scenario: List cards for a customer
    Given a customer exists with at least one card
    When I GET /cards/v1/cards/{customerID}?pageSize=100 with managed user UUID header
    Then the response status is 200
    And the response has a data array with at least one card
    And each card has id, accountId, customerId, nameOnCard, maskedPan, status, expiryDate, and productCode
    And the response includes pagination with pageNumber, pageSize, and totalPages

  Scenario: Get card details
    Given a card exists in the system
    When I GET /cards/v1/cards/{cardID}/card with managed user UUID header
    Then the response status is 200
    And the response contains the card object with all required fields
    And the card status is "Active"

  Scenario: Lock card temporarily
    Given a card with status "Active"
    When I PUT /cards/v1/cards/{cardID}/lock?reasonCode=USER_REQUEST with managed user UUID header and note "Testing lock"
    Then the response status is 200
    And the card status is changed to "TemporaryBlocked"

  Scenario: Unlock card
    Given a card with status "TemporaryBlocked"
    When I PUT /cards/v1/cards/{cardID}/unlock with managed user UUID header and note "Testing unlock"
    Then the response status is 200
    And the card status is changed back to "Active"

  Scenario: Block card permanently
    Given a card with status "Active"
    When I PUT /cards/v1/cards/{cardID}/block?reasonCode=FRAUD_SUSPECTED with managed user UUID header
    Then the response status is 200
    And the card status is changed to "Blocked"

  Scenario: Close/delete card
    Given a card with status "Active"
    When I DELETE /cards/v1/cards/{cardID}/card?reasonCode=USER_REQUEST with managed user UUID header
    Then the response status is 200
    And the card status is changed to "SoftDelete"
    And the card no longer appears in list cards query (filtered out)

  Scenario: Get card token for secure data access
    Given a card exists with id and panToken
    When I POST /cards/v1/token/card-data with managed user UUID header and cardId in body
    Then the response status is 200
    And the response contains a token starting with "mock-card-data-"
    And the token contains a link to retrieve encrypted card data

  Scenario: Get pending 3DS confirmations
    Given at least one card transaction with pending 3DS authentication
    When I GET /cards/v1/transaction/pending-confirmations with managed user UUID header
    Then the response status is 200
    And the response is an array of pending 3DS confirmations
    And each confirmation includes transactionId, merchantName, purchaseAmount, purchaseCurrency, and timeout

  Scenario: Confirm 3DS payment (approve)
    Given a pending 3DS confirmation exists with transactionId
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed true, authMethod "pin"
    Then the response status is 200
    And the response indicates success with status "approved"
    And the related transaction status changes to "COMPLETED"

  Scenario: Decline 3DS payment
    Given a pending 3DS confirmation exists with transactionId
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed false, authMethod "pin"
    Then the response status is 200
    And the response indicates declined with status "declined"
    And the related transaction status changes to "FAILED"

  Scenario: Get card application products
    When I GET /cards/v1/card-applications/test-app-id/card-products with managed user UUID header
    Then the response status is 200
    And the response contains a data array with card product objects
    And each product has id, code, name, description, type, currency, and active flag
    And the products include at least "PROD_VIRTUAL_CARD" and "PROD_PLASTIC_CARD"
    And each product has type either "VIRTUAL" or "PLASTIC"

  Scenario: Order plastic card for a card
    Given a card exists with id
    When I POST /cards/v1/cards/{cardID}/plastic with managed user UUID header
    Then the response status is 201
    And the response contains orderId, cardId, status "PENDING", and type "PLASTIC"
    And the response includes estimatedDate (7 days in the future)
    And the card object has plasticCreated flag set to true
    And the response includes a deliveryAddress with firstName, lastName, addressLine1, city, zipCode, country

  Scenario: Get card limits
    Given a card exists in the system
    When I GET /cards/v1/cards/{cardID}/limits with managed user UUID header
    Then the response status is 200
    And the response is an array of card limit objects
    And each limit has type, limit amount, currency "EUR", and isDisabled flag
    And the limits include types: dailyOverall, perTransaction, monthlyOverall, dailyAtm, dailyEcomm

  Scenario: Set card limits
    Given a card exists with default limits
    When I PUT /cards/v1/cards/{cardID}/limits with managed user UUID header and updated limits array (e.g., dailyOverall: 2000)
    Then the response status is 200
    And the response returns the updated limits array
    And the dailyOverall limit is changed to 2000

  Scenario: Create card transaction
    Given a card exists with sufficient limits and balance
    When I POST /cards/v1/transactions with cardId, amount 99.99, currency "EUR", and type "Purchase"
    Then the response status is 201
    And the response contains transactionId, cardId, transactionAmount "99.99", status "COMPLETED"
    And the transaction can be retrieved via GET /cards/v1/transactions/{transactionId}

  Scenario: Get card transaction details
    Given a card transaction exists with transactionId
    When I GET /cards/v1/transactions/{transactionId} with managed user UUID header
    Then the response status is 200
    And the response contains the full transaction object with all fields
    And the transaction includes merchantName, transactionDateTime, processingStatus
