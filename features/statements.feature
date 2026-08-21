@statements
Feature: Account and transfer statements
  As a wallet integrator
  I want downloadable statements for accounts and transfers
  So that a user can be given a record of their activity

  Background:
    Given a managed user with at least one wallet address
    And authenticated requests signed with HMAC headers

  Scenario: An account confirmation names the account it confirms
    When I GET the account confirmation for the user wallet
    Then the response status is 200
    And the response is a PDF attachment named "account-confirmation.pdf"
    And the statement reads "Account Confirmation"
    And the statement names the user wallet

  Scenario: A monthly statement reports the period and its activity
    Given the user has a funded EUR balance
    When I GET the account statement for the user wallet for the current month
    Then the response status is 200
    And the response is a PDF attachment named "account-statement.pdf"
    And the statement reads "Account Statement"
    And the statement names the user wallet
    And the statement names the current month
    And the statement lists at least one transaction

  Scenario: A monthly statement for a quiet period says so
    When I GET the account statement for the user wallet for "1999/1"
    Then the response status is 200
    And the statement reads "No activity in this period"

  Scenario Outline: A statement for a nonsensical period is refused
    When I GET the account statement for the user wallet for "<period>"
    Then the response status is 400
    And the response is not a PDF

    Examples:
      | period     |
      | 2026/13    |
      | 2026/0     |
      | 2026/March |
      | abcd/3     |

  Scenario: A transfer confirmation reports the transfer's figures
    Given the user has made an external deposit of 123.45 EUR
    When I GET the transfer confirmation for that transaction
    Then the response status is 200
    And the response is a PDF attachment named "transfer-confirmation.pdf"
    And the statement reads "Transfer Confirmation"
    And the statement reports the transaction amount "123.45 EUR"

  Scenario: A transfer confirmation is refused for a transaction with no transfer
    Given the user has made a hosted transfer of 50.00 EUR
    When I GET the transfer confirmation for that transaction
    Then the response status is 400
    And the response is not a PDF

  Scenario: A transfer confirmation for an unknown transaction is not found
    When I GET the transfer confirmation for transaction "no-such-transaction"
    Then the response status is 404

  Scenario: Statements require authentication
    When I GET the account confirmation for the user wallet without any HMAC headers
    Then the response status is 401
