Feature: Rates and vault metadata
  As a client application
  I want to retrieve exchange rates and vault details
  So that I can display pricing and route deposits correctly

  Background:
    Given MockGatehub is running and requests include valid HMAC headers

  Scenario: Get current exchange rates
    When I GET /rates/v1/rates/current
    Then the response status is 200
    And the payload contains a counter currency
    And at least one currency rate entry besides the counter field

  Scenario: Rates with counter=USD return USD-denominated values
    When I GET /rates/v1/rates/current?counter=USD&amount=1&useAll=true
    Then the response status is 200
    And the counter field is "USD"
    And the rate for "USD" is 1.0
    And the rate for "EUR" is 1.08

  Scenario: Rates with counter=EUR are converted relative to EUR
    When I GET /rates/v1/rates/current?counter=EUR&amount=1&useAll=true
    Then the response status is 200
    And the counter field is "EUR"
    And the rate for "EUR" is 1.0
    And the rate for "USD" is approximately 0.9259 within 0.001

  Scenario: Cross-currency rate consistency between counter=USD and counter=EUR
    When I GET /rates/v1/rates/current?counter=USD&amount=1&useAll=true
    Then the response status is 200
    And I save the rate for "EUR" as "eur_in_usd"
    When I GET /rates/v1/rates/current?counter=EUR&amount=1&useAll=true
    Then the response status is 200
    And the rate for "USD" is approximately the inverse of "eur_in_usd" within 0.001

  Scenario: Get liquidity provider vaults
    When I GET /rates/v1/liquidity_provider/vaults
    Then the response status is 200
    And the response includes a non-empty vaults array