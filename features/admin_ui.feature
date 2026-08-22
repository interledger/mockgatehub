@ui
Feature: Admin developer UI
  As a developer working against MockGatehub
  I want to drive its behaviour from a browser
  So that I can set up and inspect state without writing requests by hand

  Background:
    Given a managed user with at least one wallet address
    And authenticated requests signed with HMAC headers

  Scenario: The user listing shows accounts and their balances
    Given the user has a funded EUR balance
    When I browse to "/ui"
    Then the response status is 200
    And the response is an HTML page
    And the page shows "the user id"
    And the page shows "EUR"

  Scenario: The user detail page shows balances and transactions
    Given the user has made an external deposit of 55.00 EUR
    When I browse to the user detail page
    Then the response status is 200
    And the response is an HTML page
    And the page shows "Balances"
    And the page shows "Transactions"

  Scenario: The user detail page for an unknown user is not found
    When I browse to "/ui/users/no-such-user"
    Then the response status is 404

  Scenario: The admin UI needs no credentials
    # It is a browser-facing developer tool with nothing to sign with.
    When I browse to "/ui"
    Then the response status is 200

  # The admin surface is served on its own listener so it can be closed off at
  # the network level. If any of it were reachable on the application port, a
  # firewall on the admin port alone would not actually close the hole.
  Scenario Outline: The admin surface is not served on the application port
    When I request "<path>" on the application port
    Then the path is not served there

    Examples:
      | path                                     |
      | /ui                                      |
      | /ui/actions/kyc                          |
      | /admin/fees                              |
      | /admin/card-transactions/scenarios       |
      | /admin/received-webhooks                 |
      | /test-webhook                            |

  Scenario Outline: The application API is not served on the admin port
    When I request "<path>" on the admin port
    Then the path is not served there

    Examples:
      | path                              |
      | /iframe/onboarding                |
      | /cards/v1/token/pin/public-key    |
      | /rates/v1/liquidity_provider/vaults |

  Scenario: The KYC form offers every outcome
    When I browse to "/ui/actions/kyc"
    Then the response status is 200
    And the page shows "accepted"
    And the page shows "rejected"
    And the page shows "resubmission"
    And the page shows "id.document_notice.expired"

  Scenario: Sending a KYC event from the UI moves the user and announces it
    Given the webhook sink is empty
    When I submit the UI KYC action with outcome "rejected"
    Then the UI redirects reporting success
    And the redirect reports "id.verification.rejected"
    And GET /id/v1/users/{userId} shows kyc_state "rejected" and risk_level "low"
    And the "id.verification.rejected" webhook reports verdict "rejected" with status 2

  Scenario: A document notice from the UI does not change the verification state
    Given the webhook sink is empty
    When the user KYC state is quietly set to "accepted"
    And I submit the UI KYC action with outcome "id.document_notice.expired"
    Then the UI redirects reporting success
    And GET /id/v1/users/{userId} shows kyc_state "accepted" and risk_level "low"
    And the "id.document_notice.expired" webhook carries no verification verdict

  Scenario: An unrecognised KYC outcome is reported back as a failure
    When I submit the UI KYC action with outcome "probably-fine"
    Then the UI redirects reporting failure

  Scenario: The card transaction form is driven by the catalogue
    When I browse to "/ui/actions/card-transaction"
    Then the response status is 200
    And the page offers every card transaction scenario

  Scenario: Simulating a card transaction from the UI creates it and announces it
    Given the webhook sink is empty
    And the user KYC state is quietly set to "accepted"
    And a managed customer with a card exists
    When I submit the UI card transaction for scenario "withdrawal.authorization.atm-withdrawal"
    Then the UI redirects reporting success
    And the redirect reports "withdrawal.authorization.atm-withdrawal"
    And I GET the card transactions page ""
    And the listing holds 1 of 1 transactions across 1 pages

  Scenario: The card transaction form lists the chosen user's cards
    Given the user KYC state is quietly set to "accepted"
    And a managed customer with a card exists
    When I browse to the card transaction form for the user
    Then the response status is 200
    And the page shows "the card id"

  Scenario: An unknown scenario is reported back as a failure
    Given the user KYC state is quietly set to "accepted"
    And a managed customer with a card exists
    When I submit the UI card transaction for scenario "not-a-real-scenario"
    Then the UI redirects reporting failure

  Scenario: A pending withdrawal can be settled from the UI
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    And I record the user's EUR balance
    When I request a withdrawal of "120.00" EUR
    And I browse to the user detail page
    Then the page shows "Complete"
    When I submit the UI withdrawal settlement "core.withdrawal.completed"
    Then the UI redirects reporting success
    And the withdrawal status is "completed"
    And the user's EUR balance has decreased by 120.00

  Scenario: The UI does not offer to settle an already settled withdrawal
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    When I request a withdrawal of "30.00" EUR
    And I submit the UI withdrawal settlement "more-bridge.withdrawal.rejected"
    Then the UI redirects reporting success
    When I browse to the user detail page
    Then the page does not show "Complete"

  Scenario: Settling the same withdrawal twice is reported back as a failure
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    When I request a withdrawal of "40.00" EUR
    And I submit the UI withdrawal settlement "core.withdrawal.completed"
    Then the UI redirects reporting success
    And I record the user's EUR balance
    When I submit the UI withdrawal settlement "core.withdrawal.completed"
    Then the UI redirects reporting failure
    And the user's EUR balance is unchanged
