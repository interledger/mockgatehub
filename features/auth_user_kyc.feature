@kyc
Feature: Managed user authentication and KYC
  As a wallet integrator
  I want to manage users and their verification flow
  So that downstream payments can rely on authenticated, verified identities

  Background:
    Given a clean MockGatehub instance
    And HMAC headers using app id "local-test-app-id" and secret "local-test-app-secret"

  Scenario: Create a managed user
    When I POST /auth/v1/users/managed with email "testuser@example.com"
    Then the response status is 201
    And the payload includes a generated user id
    And the managed flag is true

  Scenario: Start KYC sets the user to action_required
    Given an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    Then the response contains a token for the KYC flow
    And GET /id/v1/users/{userId} shows kyc_state "action_required" and risk_level "low"

  Scenario: KYC iframe is served for onboarding
    When I GET /iframe/onboarding?token={token}&user_id={userId}
    Then the response is HTML that mentions "KYC Verification" and "MockGatehub"

  Scenario: KYC submission without 2FA proceeds normally
    Given an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    And I submit the KYC form for user {userId} without 2FA
    Then the response status is 200
    And GET /id/v1/users/{userId} shows kyc_state "accepted" and risk_level "low"

  Scenario: KYC submission with 2FA TOTP but no org callback URL
    Given an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    And I submit the KYC form for user {userId} with 2FA and code "123456"
    Then the response status is 400

  Scenario Outline: KYC submission honours the requested outcome
    Given an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    And I submit the KYC form for user {userId} with outcome "<outcome>"
    Then the response status is 200
    And the response body contains "status":"<outcome>"
    And GET /id/v1/users/{userId} shows kyc_state "<outcome>" and risk_level "low"

    Examples:
      | outcome         |
      | accepted        |
      | rejected        |
      | action_required |
      | resubmission    |

  Scenario: An unrecognised KYC outcome is refused rather than treated as a pass
    Given an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    And I submit the KYC form for user {userId} with outcome "definitely-verified"
    Then the response status is 400
    And GET /id/v1/users/{userId} shows kyc_state "action_required" and risk_level "low"

  Scenario Outline: The verification record agrees with the KYC state
    Given an existing managed user
    When the user KYC state is quietly set to "<state>"
    Then the response status is 200
    And the user verification reports status <status> and state <state_value>

    Examples:
      | state           | status | state_value |
      | accepted        | 1      | 1           |
      | rejected        | 2      | 0           |
      | resubmission    | 10     | 0           |
      | action_required | 0      | 0           |

  Scenario: A user is identified under both id and uuid
    Given an existing managed user
    Then the user reports the same identifier as both id and uuid

  Scenario: Quietly setting a KYC state emits no webhook
    Given the webhook sink is empty
    And an existing managed user
    When the user KYC state is quietly set to "rejected"
    Then the response status is 200
    And no webhook is delivered for the user

  Scenario: An unknown KYC state is refused
    Given an existing managed user
    When the user KYC state is quietly set to "verified-ish"
    Then the response status is 400

  Scenario Outline: A completed KYC announces its verdict
    Given the webhook sink is empty
    And an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    And I submit the KYC form for user {userId} with outcome "<outcome>"
    Then the response status is 200
    And the "<event>" webhook reports verdict "<short>" with status <status>

    Examples:
      | outcome         | event                              | short           | status |
      | accepted        | id.verification.accepted           | accepted        | 1      |
      | rejected        | id.verification.rejected           | rejected        | 2      |
      | action_required | id.verification.action_required    | action_required | 0      |

  Scenario: A resubmission request is not reported as a verification verdict
    Given the webhook sink is empty
    And an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    And I submit the KYC form for user {userId} with outcome "resubmission"
    Then the response status is 200
    And the "id.verification.resubmission" webhook carries no verification verdict

  Scenario: Re-verifying after a resubmission request does not announce acceptance
    Given the webhook sink is empty
    And an existing managed user
    When the user KYC state is quietly set to "resubmission"
    And I submit the KYC form for user {userId} with outcome "accepted"
    Then the response status is 200
    And GET /id/v1/users/{userId} shows kyc_state "accepted" and risk_level "low"
    And no webhook is delivered for the user
