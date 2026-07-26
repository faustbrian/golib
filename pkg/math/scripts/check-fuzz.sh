#!/bin/sh
set -eu

duration=${1:-2s}
go test ./... -run '^$'
go test ./integer -run '^$' -fuzz '^FuzzParseAndArithmetic$' -fuzztime="$duration"
go test ./rational -run '^$' -fuzz '^FuzzParseRoundTrip$' -fuzztime="$duration"
go test ./decimal -run '^$' -fuzz '^FuzzParseContextAndRoundTrip$' -fuzztime="$duration"
go test ./decimal -run '^$' -fuzz '^FuzzJSONDecoding$' -fuzztime="$duration"
go test ./bigfloat -run '^$' -fuzz '^FuzzParse$' -fuzztime="$duration"
go test ./encoding -run '^$' -fuzz '^FuzzBinaryDecoders$' -fuzztime="$duration"
