package main

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

var opts = godog.Options{
	Output: colors.Colored(os.Stdout),
	Format: "progress",
	Paths:  []string{"../docs/features"},
}

func TestFeatures(t *testing.T) {
	// Start services
	if err := startServices(); err != nil {
		t.Fatalf("Failed to start services: %v", err)
	}
	defer cleanup()

	// Wait for services to be ready
	if err := waitForServices(); err != nil {
		t.Fatalf("Services failed to start: %v", err)
	}

	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options:             &opts,
	}

	if status := suite.Run(); status != 0 {
		t.Fatalf("one or more scenarios failed")
	}
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := &TestContext{}
	tc.Reset()

	// Health check steps
	ctx.Step(`^MockGatehub is started via docker-compose in the test environment$`, tc.mockgatehubStarted)
	ctx.Step(`^the service URL is (.+)$`, tc.serviceURLIs)
	ctx.Step(`^I send a GET request to (.+) with valid HMAC headers$`, tc.sendGetRequestWithHMAC)
	ctx.Step(`^the response status is (\d+)$`, tc.responseStatusIs)
	ctx.Step(`^the response body contains "([^"]*)":"([^"]*)"$`, tc.responseBodyContains)

	// Auth steps
	ctx.Step(`^a clean MockGatehub instance$`, tc.cleanMockGatehub)
	ctx.Step(`^HMAC headers using app id "([^"]*)" and secret "([^"]*)"$`, tc.hmacHeaders)
	ctx.Step(`^I POST (.+) with email "([^"]*)"$`, tc.postWithEmail)
	ctx.Step(`^the payload includes a generated user id$`, tc.payloadHasUserID)
	ctx.Step(`^the managed flag is true$`, tc.managedFlagTrue)
	ctx.Step(`^an existing managed user$`, tc.existingManagedUserGeneric)
	ctx.Step(`^I POST (/[^ ]+)$`, tc.postEndpoint)
	ctx.Step(`^the response contains a token for the KYC flow$`, tc.responseHasKYCToken)
	ctx.Step(`^GET (.+) shows kyc_state "([^"]*)" and risk_level "([^"]*)"$`, tc.getShowsKYCState)
	ctx.Step(`^I GET (/[^ ]+)$`, tc.getEndpoint)
	ctx.Step(`^the response is HTML that mentions "([^"]*)" and "([^"]*)"$`, tc.responseIsHTMLMentioning)

	// Wallet steps
	ctx.Step(`^an authenticated request using HMAC headers$`, tc.authenticatedRequest)
	ctx.Step(`^a managed user id produced by (.+)$`, tc.managedUserFromEndpoint)
	ctx.Step(`^a wallets array is returned with at least one wallet$`, tc.walletsArrayReturned)
	ctx.Step(`^the first wallet address starts with "([^"]*)"$`, tc.firstWalletStartsWith)

	// Rates steps
	ctx.Step(`^MockGatehub is running and requests include valid HMAC headers$`, tc.mockgatehubRunningWithHeaders)
	ctx.Step(`^the payload contains a counter currency$`, tc.payloadHasCounterCurrency)
	ctx.Step(`^at least one currency rate entry besides the counter field$`, tc.rateEntryExists)
	ctx.Step(`^the response includes a non-empty vaults array$`, tc.responseHasVaults)

	// Card steps
	ctx.Step(`^a managed user with KYC state "([^"]*)"$`, tc.existingManagedUserWithKYC)
	ctx.Step(`^I POST (.+) with managed user UUID header, nameOnCard "([^"]*)", account productCode "([^"]*)", currency "([^"]*)", and card productCode "([^"]*)"$`, tc.postCardCustomerFromString)
	ctx.Step(`^the response contains a customer with id, sourceId, type "([^"]*)", and kycStatus "([^"]*)"$`, tc.responseHasCustomer)
	ctx.Step(`^the customer has one account with currency "([^"]*)", status "([^"]*)", and type "([^"]*)"$`, tc.customerHasAccount)
	ctx.Step(`^the account has one card with status "([^"]*)", nameOnCard "([^"]*)", and expiryDate in the future$`, tc.accountHasCardHelper)
	ctx.Step(`^a cards.card.created webhook is sent$`, tc.cardCreatedWebhookSent)
	ctx.Step(`^a customer exists with at least one card$`, tc.customerWithCard)
	ctx.Step(`^I GET (.+) with managed user UUID header$`, tc.getWithManagedUserHeader)
	ctx.Step(`^the response has a data array with at least one card$`, tc.responseHasCardData)
	ctx.Step(`^each card has id, accountId, customerId, nameOnCard, maskedPan, status, expiryDate, and productCode$`, tc.eachCardHasFields)
	ctx.Step(`^the response includes pagination with pageNumber, pageSize, and totalPages$`, tc.responsePaginated)
	ctx.Step(`^a card exists in the system$`, tc.cardExists)
	ctx.Step(`^the response contains the card object with all required fields$`, tc.responseHasCardObject)
	ctx.Step(`^the card status is "([^"]*)"$`, tc.cardStatusIs)
	ctx.Step(`^a card with status "([^"]*)"$`, tc.cardWithStatus)
	ctx.Step(`^I PUT (.+) with managed user UUID header and note "([^"]*)"$`, tc.putWithNote)
	ctx.Step(`^the card status is changed to "([^"]*)"$`, tc.cardStatusChangedTo)
	ctx.Step(`^the card status is changed back to "([^"]*)"$`, tc.cardStatusChangedBackTo)

	// Additional card steps
	ctx.Step(`^I DELETE (.+) with managed user UUID header$`, tc.deleteWithManagedUserHeader)
	ctx.Step(`^I PUT (.+) with managed user UUID header$`, tc.putWithManagedUserHeader)
	ctx.Step(`^the response contains a token starting with "([^"]*)"$`, tc.responseContainsTokenStarting)
	ctx.Step(`^the token contains a link to retrieve encrypted card data$`, tc.tokenContainsLink)
	ctx.Step(`^the response is an array of pending (\d+)DS confirmations$`, tc.responseIsArrayOfPending3DSConfirmations)
	ctx.Step(`^each confirmation includes transactionId, merchantName, purchaseAmount, purchaseCurrency, and timeout$`, tc.eachConfirmationHasRequiredFields)
	ctx.Step(`^I POST (.+) with managed user UUID header, confirmed (true|false), authMethod "([^"]*)"$`, tc.postWith3DSConfirmation)
	ctx.Step(`^the response indicates success with status "([^"]*)"$`, tc.responseIndicatesSuccess)
	ctx.Step(`^the response indicates declined with status "([^"]*)"$`, tc.responseIndicatesDeclined)
	ctx.Step(`^the related transaction status changes to "([^"]*)"$`, tc.transactionStatusChangesTo)
	ctx.Step(`^the response contains a data array with card product objects$`, tc.responseHasCardProducts)
	ctx.Step(`^each product has id, code, name, description, type, currency, and active flag$`, tc.eachProductHasRequiredFields)
	ctx.Step(`^the products include at least "([^"]*)" and "([^"]*)"$`, tc.productsIncludeAtLeast)
	ctx.Step(`^each product has type either "([^"]*)" or "([^"]*)"$`, tc.eachProductHasTypeEitherOr)
	ctx.Step(`^the response contains orderId, cardId, status "([^"]*)", and type "([^"]*)"$`, tc.responseContainsPlasticCardOrder)
	ctx.Step(`^the response includes estimatedDate \((\d+) days in the future\)$`, tc.responseIncludesEstimatedDate)
	ctx.Step(`^the card object has plasticCreated flag set to true$`, tc.cardHasPlasticCreatedFlag)
	ctx.Step(`^the response includes a deliveryAddress with firstName, lastName, addressLine(\d+), city, zipCode, country$`, tc.responseIncludesDeliveryAddress)
	ctx.Step(`^the response is an array of card limit objects$`, tc.responseIsArrayOfCardLimits)
	ctx.Step(`^each limit has type, limit amount, currency "([^"]*)", and isDisabled flag$`, tc.eachLimitHasRequiredFields)
	ctx.Step(`^the limits include types: dailyOverall, perTransaction, monthlyOverall, dailyAtm, dailyEcomm$`, tc.limitsIncludeRequiredTypes)
	ctx.Step(`^the response returns the updated limits array$`, tc.responseReturnsUpdatedLimits)
	ctx.Step(`^the dailyOverall limit is changed to (\d+)$`, tc.dailyOverallLimitChanged)
	ctx.Step(`^the response contains transactionId, cardId, transactionAmount "([^"]*)", status "([^"]*)"$`, tc.responseContainsCardTransaction)
	ctx.Step(`^the transaction can be retrieved via GET \/cards\/v(\d+)\/transactions\/{transactionId}$`, tc.transactionCanBeRetrieved)
	ctx.Step(`^the response contains the full transaction object with all fields$`, tc.responseContainsFullTransaction)
	ctx.Step(`^the transaction includes merchantName, transactionDateTime, processingStatus$`, tc.transactionIncludesDetails)
	ctx.Step(`^the card no longer appears in list cards query \(filtered out\)$`, tc.cardNotInListCardsQuery)

	// Transaction steps
	ctx.Step(`^a managed user with at least one wallet address$`, tc.managedUserWithWalletAddress)
	ctx.Step(`^authenticated requests signed with HMAC headers$`, tc.authenticatedRequestsWithHMAC)
	ctx.Step(`^an iframe token obtained with scope \["([^"]*)"\] and mapped to the managed user$`, tc.iframeTokenWithScope)
	ctx.Step(`^I POST (.+) with amount "([^"]*)" and currency "([^"]*)" and Authorization header "Bearer (.+)"$`, tc.postTransactionWithAuth)
	ctx.Step(`^the user balance for EUR increases by (\d+)\.(\d+)$`, tc.userBalanceIncreasesBy)
	ctx.Step(`^I POST (.+) with user_id, amount (\d+)\.(\d+), currency "([^"]*)", type (\d+), and deposit_type "([^"]*)"$`, tc.postHostedTransfer)
	ctx.Step(`^the response includes an id or uuid$`, tc.responseHasIDOrUUID)
	ctx.Step(`^the amount echoes (\d+)\.(\d+)$`, tc.amountEchoes)
	ctx.Step(`^the status is completed \(integer (\d+) or string "([^"]*)"\)$`, tc.statusIsCompleted)
	ctx.Step(`^deposit_type is "([^"]*)"$`, tc.depositTypeIs)
	ctx.Step(`^a core\.deposit\.completed webhook is emitted for the user$`, tc.coreDepositWebhookEmitted)
	ctx.Step(`^I POST (.+) with type (\d+), deposit_type "([^"]*)", receiving_address set to a wallet, amount (\d+)\.(\d+), currency "([^"]*)", and a valid vault_uuid$`, tc.postExternalDeposit)
	ctx.Step(`^fields amount, total_amount, and fee are string formatted with two decimals$`, tc.fieldsAreStringFormatted)
	ctx.Step(`^status is integer (\d+)$`, tc.statusIsInteger)
	ctx.Step(`^the transaction can be retrieved via GET \/core\/v(\d+)\/transactions\/{id} with the same format$`, tc.transactionCanBeRetrievedFormatted)
}
