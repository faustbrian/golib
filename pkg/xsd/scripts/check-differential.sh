#!/usr/bin/env bash
set -euo pipefail

go test . -run '^TestReferenceDifferentialCorpus$' -count=1

./scripts/run-java-reference.sh differential
