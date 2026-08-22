package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func startServices() error {
	cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "up", "-d", "--build", "--force-recreate")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// dumpLogs saves all container logs to testenv/lastlogs.txt for post-mortem analysis
func dumpLogs() {
	cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "logs", "--no-color")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to dump container logs: %v\n", err)
		return
	}
	if err := os.WriteFile("lastlogs.txt", output, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write lastlogs.txt: %v\n", err)
	}
}

func cleanup() {
	// Always dump logs before tearing down containers
	dumpLogs()

	if os.Getenv("KEEP_CONTAINERS") != "" {
		fmt.Println("KEEP_CONTAINERS is set — skipping container teardown")
		return
	}

	cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "down", "-v")
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
}

func waitForServices() error {
	// Both listeners on both instances: a scenario that reaches the admin
	// surface before it is up would fail for the wrong reason.
	for _, url := range []string{
		mockGatehubURL, mockGatehubAdminURL,
		asyncWithdrawalsURL, asyncWithdrawalsAdminURL,
	} {
		if err := waitForService(url); err != nil {
			return err
		}
	}
	return nil
}

func waitForService(baseURL string) error {
	// http.Get uses http.DefaultClient, which has no timeout. A hanging
	// connection would then block far longer than maxWaitSeconds and leave the
	// harness looking stuck rather than reporting a failed startup.
	client := &http.Client{Timeout: healthProbeTimeout}

	for i := 0; i < maxWaitSeconds; i++ {
		resp, err := client.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("health check for %s timed out after %d seconds", baseURL, maxWaitSeconds)
}
