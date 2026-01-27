Feature: Wallet lifecycle and balances
  As a wallet developer
  I want managed users to receive XRPL wallets and accurate balances
  So that client apps can rely on consistent custody behavior

  Background:
    Given an authenticated request using HMAC headers
    And a managed user id produced by /auth/v1/users/managed

  Scenario: Auto-create wallet on first lookup
    When I GET /core/v1/users/{userId}
    Then a wallets array is returned with at least one wallet
    And the first wallet address starts with "r"
    And the address is stored for subsequent requests

  Scenario: Wallet persistence on subsequent lookups
    Given a previously returned wallet address
    When I GET /core/v1/users/{userId} again
    Then the same wallet address is returned

  Scenario: Create an additional wallet via user-scoped endpoint
    When I POST /core/v1/users/{userId}/wallets with name "My Wallet" and currency "XRP"
    Then the response status is 201
    And the payload includes a new wallet address starting with "r"

  Scenario: Create a wallet via global endpoint
    When I POST /core/v1/wallets with user_id, name, and optional network
    Then the response status is 201
    And the wallet belongs to the provided user_id

  Scenario: Retrieve balances across all currencies
    Given a wallet address
    When I GET /core/v1/wallets/{address}/balances
    Then the response lists balances for all supported currencies (11 entries)
    And each entry includes currency and vault_uuid

  Scenario: Deposits update balances in the requested currency
    Given a wallet with deposits of 500.00 USD
    When I GET /core/v1/wallets/{address}/balance
    Then the USD balance shows 500.00 available
    And vault metadata is present for USD

  Scenario Outline: Multi-currency deposits are recorded accurately
    Given a wallet belonging to a KYC-accepted user
    When I POST /core/v1/transactions with amount <amount>, currency <currency>, vault_uuid <vault>, receiving_address set to the wallet, type 1, and deposit_type "external"
    Then the transaction is created with status 1 and amount formatted to two decimals
    And a subsequent balance check reflects the deposited <amount> for <currency>

    Examples:
      | currency | amount | vault                                 |
      | USD      | 100.00 | 450d2156-132a-4d3f-88c5-74822547658d |
      | EUR      | 200.50 | a09a0a2c-1a3a-44c5-a1b9-603a6eea9341 |
      | GBP      | 75.25  | 992b932d-7e9e-44b0-90ea-b82a530b6784 |