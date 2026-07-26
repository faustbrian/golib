#!/usr/bin/env bash
set -euo pipefail

duration="${1:-5s}"
go test . -run '^$' -fuzz '^FuzzParseRoundTrip$' -fuzztime "$duration"
go test . -run '^$' -fuzz '^FuzzModelRoundTrip$' -fuzztime "$duration"
go test ./builder -run '^$' -fuzz '^FuzzBuilderRoundTrip$' -fuzztime "$duration"
