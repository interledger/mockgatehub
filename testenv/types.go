package main

const (
	mockGatehubURL = "http://localhost:25151"

	// asyncWithdrawalsURL is a second instance running with
	// MOCKGATEHUB_ASYNC_WITHDRAWALS enabled, so scenarios can cover both
	// positions of that switch.
	asyncWithdrawalsURL = "http://localhost:25152"

	maxWaitSeconds = 60
)
