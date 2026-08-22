@cards
Feature: Card management and lifecycle
  As a wallet integrator using cards
  I want to manage card customers and their cards
  So that I can provide card functionality to users

  Background:
    Given a clean MockGatehub instance
    And a managed user with KYC state "accepted"
    And HMAC headers using app id "local-test-app-id" and secret "local-test-app-secret"

  Scenario: Create a managed customer with EUR account and initial card
    When I POST /cards/v1/customers/managed with managed user UUID header, nameOnCard "John Doe", account productCode "PWSR_DEBP_2404", currency "EUR", and card productCode "PWSR_DEBP_2404"
    Then the response status is 201
    And the response contains a customer with id, sourceId, type "Citizen", and kycStatus "accepted"
    And the customer has one account with currency "EUR", status "ACTIVE", and type "DEBIT"
    And the account has one card with status "Active", nameOnCard "John Doe", and expiryDate in the future

  Scenario: List cards for a customer
    Given a managed customer with a card exists
    When I GET /cards/v1/cards/{customerId}?pageSize=100 with managed user UUID header
    Then the response status is 200
    And the response has a data array with at least one card
    And each card has id, accountId, customerId, nameOnCard, maskedPan, status, expiryDate, and productCode
    And the response includes pagination with pageNumber, pageSize, and totalPages

  Scenario: Get card details
    Given a managed customer with a card exists
    When I GET /cards/v1/cards/{cardId}/card with managed user UUID header
    Then the response status is 200
    And the response contains the card object with all required fields
    And the card status is "Active"

  Scenario: Lock card temporarily
    Given a managed customer with a card exists
    When I PUT /cards/v1/cards/{cardId}/lock?reasonCode=ClientRequestedLock with managed user UUID header and note "Testing lock"
    Then the response status is 200
    And the card status is changed to "TemporaryBlocked"

  Scenario: Unlock card
    Given a managed customer with a locked card exists
    When I PUT /cards/v1/cards/{cardId}/unlock with managed user UUID header and note "Testing unlock"
    Then the response status is 200
    And the card status is changed back to "Active"

  Scenario: Close/delete card
    Given a managed customer with a card exists
    When I DELETE /cards/v1/cards/{cardId}/card?reasonCode=UserRequest with managed user UUID header
    Then the response status is 200
    And the card no longer appears in list cards query (filtered out)

  Scenario: Get card token for secure data access
    Given a managed customer with a card exists
    And the caller has generated an RSA key pair
    When I POST /cards/v1/token/card-data with the cardId and the caller's public key
    Then the response status is 200
    And the response contains a links array with at least one entry
    And the token link is an absolute URL
    And the token link path is "/cards/v1/token/card-data/data"
    And the token link method is "GET"

  Scenario: Get card limits
    Given a managed customer with a card exists
    When I GET /cards/v1/cards/{cardId}/limits with managed user UUID header
    Then the response status is 200
    And the response is an array of card limit objects
    And each limit has type, limit amount, currency "EUR", and isDisabled flag
    And the limits include types: dailyOverall, perTransaction, monthlyOverall, dailyAtm, dailyEcomm

  Scenario: Set card limits
    Given a managed customer with a card exists
    When I PUT /cards/v1/cards/{cardId}/limits with managed user UUID header and updated dailyOverall limit 2000
    Then the response status is 200
    And the response returns the updated limits array
    And the dailyOverall limit is changed to 2000

  Scenario: Create card transaction
    Given a managed customer with a card exists
    When I POST /cards/v1/transactions with cardId, amount "25.00", currency "EUR", and managed user UUID header
    Then the response status is 201
    And the response contains a card transaction with transactionId and ghResponseCode "00"

  Scenario: Get card transaction details
    Given a managed customer with a card and transaction exists
    When I GET /cards/v1/transactions/{transactionId} with managed user UUID header
    Then the response status is 200
    And the response contains the card transaction with transactionId and transactionAmount

  Scenario: Get pending 3DS confirmations
    Given a managed customer with a pending 3DS challenge exists
    When I GET /cards/v1/transaction/pending-confirmations with managed user UUID header
    Then the response status is 200
    And the response is an array of pending 3DS confirmations
    And each confirmation includes transactionId, merchantName, purchaseAmount, purchaseCurrency, and timeout

  Scenario: Confirm 3DS payment (approve)
    Given a managed customer with a pending 3DS challenge exists
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed true, authMethod "pin"
    Then the response status is 200
    And the response contains "confirmed" with value true

  Scenario: Decline 3DS payment
    Given a managed customer with a pending 3DS challenge exists
    When I POST /cards/v1/transaction/{transactionId} with managed user UUID header, confirmed false, authMethod "pin"
    Then the response status is 200
    And the response contains "confirmed" with value false

  Scenario: Get card application products
    When I GET /cards/v1/card-applications/test-app-id/card-products with card app ID header
    Then the response status is 200
    And the response is an array of card product objects

  Scenario: Order plastic card for a card
    Given a managed customer with a card exists
    When I POST /cards/v1/cards/{cardId}/plastic with managed user UUID header
    Then the response status is 201
    And the response contains orderId, cardId, status "PENDING", and type "PLASTIC"

  Scenario: Card details are readable only by the caller that asked for them
    Given a managed customer with a card exists
    And the caller has generated an RSA key pair
    When I POST /cards/v1/token/card-data with the cardId and the caller's public key
    And the browser follows the card-data link with "the issued token"
    Then the response status is 200
    And the payload decrypts with the caller's private key to card details

  Scenario: The same card always reports the same card number
    Given a managed customer with a card exists
    And the caller has generated an RSA key pair
    When I POST /cards/v1/token/card-data with the cardId and the caller's public key
    And the browser follows the card-data link with "the issued token"
    Then the payload decrypts with the caller's private key to card details
    When I POST /cards/v1/token/card-data with the cardId and the caller's public key
    And the browser follows the card-data link with "the issued token"
    Then the decrypted card number matches the previous read

  Scenario: The card-data endpoint refuses a token it did not issue
    Given a managed customer with a card exists
    When the browser follows the card-data link with "a-forged-token"
    Then the response status is 401
    And no card data is disclosed

  Scenario: The card-data endpoint refuses a request with no token
    Given a managed customer with a card exists
    When the browser follows the card-data link with "no token"
    Then the response status is 401
    And no card data is disclosed

  Scenario: A card-data token cannot be used to read the PIN
    Given a managed customer with a card exists
    And the caller has generated an RSA key pair
    When I POST /cards/v1/token/card-data with the cardId and the caller's public key
    And the browser follows the pin link with "the issued token"
    Then the response status is 401

  Scenario: A card PIN can be read without ever having been set
    Given a managed customer with a card exists
    And the caller has generated an RSA key pair
    When the caller reads the card PIN
    Then the response status is 200
    And the decrypted PIN looks like a PIN

  Scenario: A card PIN that is set is the PIN that is read back
    Given a managed customer with a card exists
    And the caller has generated an RSA key pair
    When the caller sets the card PIN to "4321"
    Then the response status is 200
    When the caller reads the card PIN
    Then the response status is 200
    And the decrypted PIN is "4321"

  Scenario: Creating a card tells the consumer which identifiers were assigned
    Given the webhook sink is empty
    And a managed customer with a card exists
    Then a "cards.card.created" webhook reports the assigned identifiers

  Scenario: The card transaction catalogue is discoverable
    When I GET the card transaction scenario catalogue
    Then the response status is 200
    And the catalogue lists 17 scenarios
    And the catalogue includes the scenario "withdrawal.authorization.purchase-transaction-with-fx"
    And the catalogue includes the scenario "none.insufficient-balance"

  Scenario: A simulated card transaction reaches the card's transaction listing
    Given a managed customer with a card exists
    When I simulate the card transaction scenario "withdrawal.authorization.atm-withdrawal"
    Then the response status is 201
    And the simulated transaction has a numeric id and cardId
    When I GET the card transactions page ""
    Then the response status is 200
    And the listing contains the simulated transaction with its unmodelled fields intact

  Scenario: A simulated card transaction is announced with the transaction attached
    Given the webhook sink is empty
    And a managed customer with a card exists
    When I simulate the card transaction scenario "withdrawal.authorization.purchase-transaction"
    Then the response status is 201
    And the "cards.transaction.authorization" webhook carries the full transaction

  Scenario: Card transaction listings are paged
    Given a managed customer with a card exists
    And I simulate 5 card transactions of scenario "withdrawal.authorization.purchase-transaction"
    When I GET the card transactions page "pageSize=2&pageNumber=1"
    Then the response status is 200
    And the listing holds 2 of 5 transactions across 3 pages
    When I GET the card transactions page "pageSize=2&pageNumber=3"
    Then the listing holds 1 of 5 transactions across 3 pages

  Scenario: A simulated transaction's status can be advanced
    Given a managed customer with a card exists
    When I simulate the card transaction scenario "withdrawal.authorization.preauthorization"
    And I set the simulated transaction status to "COMPLETED"
    Then the response status is 200
    And the stored transaction reports txStatus "COMPLETED"

  Scenario: An unknown scenario is refused and the valid options are named
    Given a managed customer with a card exists
    When I simulate the card transaction scenario "not-a-real-scenario"
    Then the response status is 400
    And the error names the valid scenarios
