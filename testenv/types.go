package main

import "time"

const (
	mockGatehubURL = "http://localhost:25151"

	// The admin UI and the test-support endpoints are served on their own
	// listener, so a harness has to address them separately from the
	// application API.
	mockGatehubAdminURL = "http://localhost:25153"

	// asyncWithdrawalsURL is a second instance running with
	// MOCKGATEHUB_ASYNC_WITHDRAWALS enabled, so scenarios can cover both
	// positions of that switch.
	asyncWithdrawalsURL      = "http://localhost:25152"
	asyncWithdrawalsAdminURL = "http://localhost:25154"

	maxWaitSeconds = 60

	// healthProbeTimeout bounds a single startup health probe, so one hung
	// connection cannot outlast the whole maxWaitSeconds budget.
	healthProbeTimeout = 3 * time.Second
)
