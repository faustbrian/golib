#!/usr/bin/env bash
set -euo pipefail

duration="${1:-5s}"

go test . -run '^$' -fuzz '^FuzzParseSchema$' -fuzztime "$duration"
go test . -run '^$' -fuzz '^FuzzValidateInstance$' -fuzztime "$duration"
go test ./datatype -run '^$' -fuzz '^FuzzParseDecimal$' -fuzztime "$duration"
go test ./datatype -run '^$' -fuzz '^FuzzBuiltInLexical$' -fuzztime "$duration"
