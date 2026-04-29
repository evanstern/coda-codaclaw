#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
echo "Building coda-codaclaw..."
go build -o bin/coda-codaclaw ./cmd/coda-codaclaw
echo "Built: bin/coda-codaclaw"
