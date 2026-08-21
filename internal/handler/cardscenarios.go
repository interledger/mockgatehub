package handler

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// cardTransactionMocks holds a catalogue of realistic GateHub card transaction
// payloads, one per situation the card flows have to cope with. Keeping them as
// data rather than as code means a consumer can be exercised against the exact
// JSON GateHub produces, including fields this service does not model.
//
//go:embed mockdata/cardtransaction_mocks.json
var cardTransactionMocks embed.FS

const cardScenarioFile = "mockdata/cardtransaction_mocks.json"

// CardTxScenario is one entry in the catalogue.
type CardTxScenario struct {
	// Key is the stable identifier callers use to select a scenario.
	Key string `json:"key"`
	// Operation is the direction of money movement: withdrawal, deposit, or
	// none for the declined cases where nothing moves.
	Operation string `json:"operation"`
	// Classification distinguishes an authorization from a reversal. It is
	// empty for scenarios the source groups without one.
	Classification string `json:"classification"`
	// Case is the human-readable situation, e.g. "ATM Withdrawal".
	Case string `json:"case"`
	// Payload is the verbatim transaction JSON.
	Payload json.RawMessage `json:"payload"`
}

// cardScenarioIndex is the parsed catalogue, built once at startup.
var cardScenarioIndex = mustLoadCardScenarios()

// scenarioEntry is the shape of one item in the source file.
type scenarioEntry struct {
	Case    string          `json:"case"`
	Payload json.RawMessage `json:"payload"`
}

// mustLoadCardScenarios parses the embedded catalogue. A parse failure is a
// build-time mistake in a file that ships with the binary, so there is no
// sensible runtime recovery.
func mustLoadCardScenarios() []CardTxScenario {
	scenarios, err := loadCardScenarios()
	if err != nil {
		panic(fmt.Sprintf("mockgatehub: invalid embedded card scenario catalogue: %v", err))
	}
	return scenarios
}

func loadCardScenarios() ([]CardTxScenario, error) {
	raw, err := cardTransactionMocks.ReadFile(cardScenarioFile)
	if err != nil {
		return nil, err
	}

	// The source groups scenarios by operation, and then usually — but not
	// always — by classification. "Operation - None" holds a flat list because
	// a declined transaction has no classification.
	var byOperation map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byOperation); err != nil {
		return nil, fmt.Errorf("top level: %w", err)
	}

	var scenarios []CardTxScenario
	for opLabel, opBody := range byOperation {
		operation := trimLabel(opLabel, "Operation")

		var flat []scenarioEntry
		if err := json.Unmarshal(opBody, &flat); err == nil {
			for _, e := range flat {
				scenarios = append(scenarios, newScenario(operation, "", e))
			}
			continue
		}

		var byClassification map[string][]scenarioEntry
		if err := json.Unmarshal(opBody, &byClassification); err != nil {
			return nil, fmt.Errorf("operation %q: %w", opLabel, err)
		}
		for clsLabel, entries := range byClassification {
			classification := trimLabel(clsLabel, "Transaction Classification")
			for _, e := range entries {
				scenarios = append(scenarios, newScenario(operation, classification, e))
			}
		}
	}

	// Map iteration order is random; sort so listings and tests are stable.
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Key < scenarios[j].Key })

	if err := assertUniqueKeys(scenarios); err != nil {
		return nil, err
	}
	return scenarios, nil
}

func newScenario(operation, classification string, e scenarioEntry) CardTxScenario {
	return CardTxScenario{
		Key:            scenarioKey(operation, classification, e.Case),
		Operation:      operation,
		Classification: classification,
		Case:           e.Case,
		Payload:        e.Payload,
	}
}

func assertUniqueKeys(scenarios []CardTxScenario) error {
	seen := make(map[string]string, len(scenarios))
	for _, s := range scenarios {
		if prev, dup := seen[s.Key]; dup {
			return fmt.Errorf("duplicate scenario key %q (%s and %s)", s.Key, prev, s.Case)
		}
		seen[s.Key] = s.Case
	}
	return nil
}

// trimLabel strips the source file's "Prefix - " decoration from a group name.
func trimLabel(label, prefix string) string {
	return strings.TrimSpace(strings.TrimPrefix(label, prefix+" - "))
}

// scenarioKey builds a stable, URL-safe identifier. The case name alone is not
// unique — "Purchase Transaction" appears as both an authorization and a
// reversal — so the operation and classification are part of the key.
func scenarioKey(operation, classification, caseName string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{operation, classification, caseName} {
		if slug := slugify(p); slug != "" {
			parts = append(parts, slug)
		}
	}
	return strings.Join(parts, ".")
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := true // suppress a leading dash
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// listCardScenarios returns the catalogue in a stable order.
func listCardScenarios() []CardTxScenario {
	out := make([]CardTxScenario, len(cardScenarioIndex))
	copy(out, cardScenarioIndex)
	return out
}

// findCardScenario looks a scenario up by its key. Matching is
// case-insensitive so callers need not worry about how they spell it.
func findCardScenario(key string) (CardTxScenario, bool) {
	want := strings.ToLower(strings.TrimSpace(key))
	for _, s := range cardScenarioIndex {
		if s.Key == want {
			return s, true
		}
	}
	return CardTxScenario{}, false
}
