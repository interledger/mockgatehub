@contract
Feature: Consumer API contract
  As a maintainer of MockGatehub
  I want the endpoints real consumers call to keep working
  So that a change here cannot silently break a downstream integration

  # Every path below is one the testnet wallet backend calls, taken from its
  # GateHub client. They are asserted as a set so that a route rename, a prefix
  # change or a narrowed method is caught here rather than downstream.

  Background:
    Given a clean MockGatehub instance
    And HMAC headers using app id "local-test-app-id" and secret "local-test-app-secret"

  Scenario: Authentication and identity endpoints are reachable
    Given an existing managed user
    Then POST /auth/v1/tokens is served
    And GET /auth/v1/users/managed is served
    And PUT /auth/v1/users/managed/email is served
    And GET /id/v1/users/{userId} is served
    And POST /id/v1/users/{userId}/hubs/gw is served
    And PUT /id/v1/hubs/gw/users/{userId} is served
    And POST /id/v1/hubs/gw/users/{userId}/overrideRiskLevel is served

  Scenario: Core wallet endpoints are reachable
    Given a managed user with at least one wallet address
    Then GET /core/v1/users/{userId} is served
    And POST /core/v1/users/{userId}/wallets is served
    And GET /core/v1/users/{userId}/wallets/{walletAddress} is served
    And GET /core/v1/wallets/{walletAddress}/balances is served
    And POST /core/v1/transactions is served

  Scenario: Rates endpoints are reachable
    Then GET /rates/v1/liquidity_provider/vaults is served
    And GET /rates/v1/rates/current?counter=EUR&amount=1&useAll=true is served

  Scenario: Card endpoints are reachable under the paths consumers use
    Given a managed user with KYC state "accepted"
    And a managed customer with a card exists
    Then GET /cards/v1/customers/{customerId}/cards is served
    And GET /cards/v1/cards/{cardId}/card is served
    And GET /cards/v1/cards/{cardId}/transactions is served
    And PUT /cards/v1/cards/{cardId}/lock?reasonCode=ClientRequestedLock is served
    And PUT /cards/v1/cards/{cardId}/unlock is served
    And POST /cards/v1/cards/{cardId}/plastic is served

  # Consumers address these without the /cards prefix, since GateHub serves
  # them under a bare /v1 too.
  Scenario: Card limits and products are reachable under the bare /v1 prefix
    Given a managed user with KYC state "accepted"
    And a managed customer with a card exists
    Then GET /v1/cards/{cardId}/limits is served
    And POST /v1/cards/{cardId}/limits is served
    And GET /v1/card-applications/test-app-id/card-products is served

  Scenario: All three card token types are reachable
    # A consumer calls card-data, pin and pin-change through the same route.
    # Narrowing it to one type would 404 the other two.
    Given a managed user with KYC state "accepted"
    And a managed customer with a card exists
    And the caller has generated an RSA key pair
    Then POST /cards/v1/token/card-data is served
    And POST /cards/v1/token/pin is served
    And POST /cards/v1/token/pin-change is served

  Scenario: The card object keeps the fields consumers read
    Given a managed user with KYC state "accepted"
    And a managed customer with a card exists
    When I GET /cards/v1/cards/{cardId}/card with managed user UUID header
    Then the response status is 200
    And the card object carries the fields consumers read

  Scenario: A card transaction keeps the fields consumers read
    Given a managed user with KYC state "accepted"
    And a managed customer with a card exists
    When I simulate the card transaction scenario "withdrawal.authorization.purchase-transaction"
    And I GET the card transactions page ""
    Then the response status is 200
    And each listed card transaction carries the fields consumers read

  Scenario: The user state response keeps the fields consumers read
    Given an existing managed user
    When I GET /id/v1/users/{userId}
    Then the response status is 200
    And the user state carries the profile and verifications consumers read
