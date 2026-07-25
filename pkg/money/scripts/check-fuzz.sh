#!/usr/bin/env bash
set -euo pipefail

duration="${FUZZ_TIME:-5s}"

go test . -run '^$' -parallel 2 -fuzz FuzzParseMoney -fuzztime "$duration"
go test . -run '^$' -parallel 2 -fuzz FuzzAllocationConservation -fuzztime "$duration"
go test . -run '^$' -parallel 2 -fuzz FuzzWeightedAllocationConservation -fuzztime "$duration"
go test . -run '^$' -parallel 2 -fuzz FuzzRate -fuzztime "$duration"
go test ./encoding -run '^$' -parallel 2 -fuzz FuzzVersionedJSON -fuzztime "$duration"
go test ./encoding -run '^$' -parallel 2 -fuzz FuzzPostgreSQLNumeric -fuzztime "$duration"
go test ./format -run '^$' -parallel 2 -fuzz FuzzLocale -fuzztime "$duration"
