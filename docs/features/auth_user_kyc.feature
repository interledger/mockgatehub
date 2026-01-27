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

  Scenario: Obtain an access token for the managed user
    Given an existing managed user with email "testuser@example.com"
    When I POST /auth/v1/tokens with username "testuser@example.com" and password "TestPass123!"
    Then the response contains an access_token string

  Scenario: Obtain an iframe token mapped to a managed user
    Given an existing managed user
    When I POST /auth/v1/tokens?clientId=test-client-id with scope ["deposit"] and header x-gatehub-managed-user-uuid set to that user id
    Then the response contains a token prefixed with "iframe-token-"
    And the token can be reused as a bearer for deposit flows

  Scenario: Start KYC sets the user to action_required
    Given an existing managed user
    When I POST /id/v1/users/{userId}/hubs/gw
    Then the response contains a token for the KYC flow
    And GET /id/v1/users/{userId} shows kyc_state "action_required" and risk_level "low"

  Scenario: KYC iframe is served for onboarding
    When I GET /iframe/onboarding?token={token}&user_id={userId}
    Then the response is HTML that mentions "KYC Verification" and "MockGatehub"

  Scenario: Submitting KYC form approves the user
    Given a user whose KYC state is action_required
    When I POST /iframe/submit with form fields first_name, last_name, dob, address, city, country, and risk_level=low
    Then GET /id/v1/users/{userId} returns kyc_state "accepted"