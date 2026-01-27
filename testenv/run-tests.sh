#!/bin/bash
# Run MockGatehub integration tests using Go

set -e

docker compose build --no-cache
docker compose up -d

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Build the test binary with legacy tag
echo "Building test binary (legacy)..."
go build -tags legacy_testenv -o testscript testscript.go

# Run the tests
./testscript

# Exit with the test exit code
exit $?
