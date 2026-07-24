#!/bin/sh
set -eu

duration=${FUZZ_TIME:-10s}
go test -run '^$' -fuzz '^FuzzAnalyzeAlphabet$' -fuzztime="$duration" ./password
go test -run '^$' -fuzz '^FuzzValidation$' -fuzztime="$duration" ./wordlist
go test -run '^$' -fuzz '^FuzzMnemonicParsing$' -fuzztime="$duration" ./bip39
go test -run '^$' -fuzz '^FuzzParsing$' -fuzztime="$duration" ./passphrase
