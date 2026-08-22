@withdrawals
Feature: Withdrawal settlement
  As a wallet integrator
  I want withdrawals to settle the way my integration expects
  So that a user's balance always reflects what actually happened

  Background:
    Given a managed user with at least one wallet address
    And authenticated requests signed with HMAC headers
    And the user has a funded EUR balance

  # The default. Consumers that handle no withdrawal webhooks learn the outcome
  # from the response, so a withdrawal left pending would never complete.
  Scenario: By default a withdrawal settles immediately and charges the balance
    Given I record the user's EUR balance
    When I request a withdrawal of "200.00" EUR
    Then the response status is 200
    And the response body contains "message":"Withdrawal completed"
    And the withdrawal status is "completed"
    And the user's EUR balance has decreased by 200.00

  Scenario: A withdrawal larger than the balance is refused
    Given I record the user's EUR balance
    When I request a withdrawal of "999999.00" EUR
    Then the response status is 400
    And the user's EUR balance is unchanged

  Scenario: A withdrawal records where the money is going
    When I request a withdrawal of "20.00" EUR
    Then the response status is 200
    And I GET the withdrawals filtered by ""
    And the withdrawal listing names the destination bank details

  # The opt-in behaviour, on a separate instance with the switch enabled.
  Scenario: When asynchronous, a withdrawal stays pending and charges nothing
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    And I record the user's EUR balance
    When I request a withdrawal of "200.00" EUR
    Then the response status is 200
    And the response body contains "message":"Withdrawal pending"
    And the withdrawal status is "pending"
    And the user's EUR balance is unchanged

  Scenario: Completing a pending withdrawal charges the balance and announces it
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    And the webhook sink is empty
    And I record the user's EUR balance
    When I request a withdrawal of "150.00" EUR
    And I trigger the withdrawal event "core.withdrawal.completed"
    Then the response status is 200
    And the withdrawal status is "completed"
    And the user's EUR balance has decreased by 150.00
    And the "core.withdrawal.completed" webhook reports the withdrawal

  Scenario: Rejecting a pending withdrawal leaves the balance untouched
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    And the webhook sink is empty
    And I record the user's EUR balance
    When I request a withdrawal of "150.00" EUR
    And I trigger the withdrawal event "more-bridge.withdrawal.rejected"
    Then the response status is 200
    And the withdrawal status is "failed"
    And the user's EUR balance is unchanged
    And the "more-bridge.withdrawal.rejected" webhook reports the withdrawal

  Scenario: A settled withdrawal cannot be settled again
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    When I request a withdrawal of "100.00" EUR
    And I trigger the withdrawal event "core.withdrawal.completed"
    Then the response status is 200
    And I record the user's EUR balance
    When I trigger the withdrawal event "core.withdrawal.completed"
    Then the response status is 409
    And the user's EUR balance is unchanged

  Scenario: A rejected withdrawal cannot then be completed
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    When I request a withdrawal of "100.00" EUR
    And I trigger the withdrawal event "more-bridge.withdrawal.rejected"
    Then the response status is 200
    When I trigger the withdrawal event "core.withdrawal.completed"
    Then the response status is 409
    And the withdrawal status is "failed"

  Scenario: Pending withdrawals can be listed on their own
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    When I request a withdrawal of "10.00" EUR
    And I trigger the withdrawal event "core.withdrawal.completed"
    And I request a withdrawal of "20.00" EUR
    And I GET the withdrawals filtered by "pending"
    Then the response status is 200
    And the withdrawal listing holds 1 withdrawal

  Scenario: An unrecognised status filter is refused
    When I GET the withdrawals filtered by "nearly-done"
    Then the response status is 400

  Scenario: An unsupported settlement event is refused
    Given requests go to the asynchronous withdrawals instance
    And a managed user with at least one wallet address
    And the user has a funded EUR balance
    When I request a withdrawal of "10.00" EUR
    And I trigger the withdrawal event "core.withdrawal.invented"
    Then the response status is 400
    And the withdrawal status is "pending"

  Scenario: Settling an unknown withdrawal is not found
    When I trigger the withdrawal event "core.withdrawal.completed" for transaction "no-such-withdrawal"
    Then the response status is 404
