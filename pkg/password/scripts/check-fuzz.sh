#!/bin/sh
set -eu

duration=${FUZZ_TIME:-10s}
go test -run '^$' -fuzz '^FuzzParseEncodedHash$' -fuzztime="$duration" ./
go test -run '^$' -fuzz '^FuzzBoundedVerify$' -fuzztime="$duration" ./
