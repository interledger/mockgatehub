package main

func allScenarios() []scenario {
    return []scenario{
        scenarioUserKYCAcceptance(),
        scenarioWalletsAndBalances(),
        scenarioExternalDeposit(),
        scenarioHostedTransfer(),
        scenarioIframeDeposit(),
        scenarioRatesAndVaults(),
    }
}
