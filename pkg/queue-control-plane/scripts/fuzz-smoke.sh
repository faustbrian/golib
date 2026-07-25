#!/usr/bin/env bash
set -euo pipefail

go test ./apihttp \
    -run '^$' \
    -fuzz '^FuzzCommandHandlerFailsClosed$' \
    -fuzztime "${FUZZ_TIME:-5s}"
go test ./apihttp \
    -run '^$' \
    -fuzz '^FuzzRecordHandlerFailsClosed$' \
    -fuzztime "${FUZZ_TIME:-5s}"
go test ./apihttp \
    -run '^$' \
    -fuzz '^FuzzQueueHandlerFailsClosed$' \
    -fuzztime "${FUZZ_TIME:-5s}"
