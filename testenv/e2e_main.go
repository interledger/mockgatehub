package main

import (
    "fmt"
    "net/http"
    "os"
)

func main() {
    fmt.Println("=== MockGatehub E2E Harness (testenv) ===")

    if err := startServices(); err != nil {
        fmt.Fprintf(os.Stderr, "failed to start services: %v\n", err)
        os.Exit(1)
    }
    defer cleanup()

    if err := waitForServices(); err != nil {
        fmt.Fprintf(os.Stderr, "services not ready: %v\n", err)
        os.Exit(1)
    }

    h := &harness{client: http.DefaultClient}
    scenarios := allScenarios()

    passed := 0
    for i, sc := range scenarios {
        fmt.Printf("\n[%d/%d] %s\n", i+1, len(scenarios), sc.name)
        if err := flushRedis(); err != nil {
            fmt.Printf("- FLUSH redis: ERROR (%v)\n", err)
            os.Exit(1)
        }
        msg, err := sc.run(h)
        if err != nil {
            fmt.Printf("- FAIL: %v\n", err)
            os.Exit(1)
        }
        passed++
        if msg != "" {
            fmt.Printf("- PASS: %s\n", msg)
        } else {
            fmt.Println("- PASS")
        }
    }

    fmt.Printf("\nAll scenarios passed (%d/%d).\n", passed, len(scenarios))
}
