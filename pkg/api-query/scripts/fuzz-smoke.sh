#!/bin/sh
set -eu

fuzztime=${FUZZTIME:-5s}
go test -run '^$' -fuzz '^FuzzCompileFilterExpression$' -fuzztime="$fuzztime" .
go test -run '^$' -fuzz '^FuzzParse$' -fuzztime="$fuzztime" ./apiqueryhttp
go test -run '^$' -fuzz '^FuzzParse$' -fuzztime="$fuzztime" ./apiqueryrpc
go test -run '^$' -fuzz '^FuzzDecode$' -fuzztime="$fuzztime" ./cursor
