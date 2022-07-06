Feature: Exposing unsecure API and then securing it with OAuth2

  Scenario: Securing an unsecured API with OAuth2 and calling it without a token
    Given SecureToUnsecure: There is an endpoint secured with OAuth2
    And SecureToUnsecure: The endpoint is reachable
    When SecureToUnsecure: Endpoint is exposed with noop strategy
    Then SecureToUnsecure: Calling the endpoint without a token should result in status beetween 200 and 299
    And SecureToUnsecure: Calling the endpoint with any token should result in status beetween 200 and 299
