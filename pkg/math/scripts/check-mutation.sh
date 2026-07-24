#!/bin/sh
set -eu

go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash . \
	--workers "${MUTATION_WORKERS:-8}" \
	--test-cpu "${MUTATION_TEST_CPU:-2}" \
	--timeout-coefficient "${MUTATION_TIMEOUT_COEFFICIENT:-10}" \
	--threshold-efficacy 100 \
	--threshold-mcover 100 \
	--output-statuses l
