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
