#!/bin/sh
set -eu

if rg -n '"C"|unsafe\.' --glob '*.go' --glob '!**/*_test.go' .; then
	printf 'unsafe or cgo use is forbidden\n' >&2
	exit 1
fi
if rg -n 'math/rand' --glob '*.go' --glob '!**/*_test.go' .; then
	printf 'ambient pseudo-randomness is forbidden\n' >&2
	exit 1
fi
if rg -n '\bgo[[:space:]]+([[:alnum:]_]+\.)?[[:alnum:]_]+\(' \
	--glob '*.go' --glob '!**/*_test.go' .; then
	printf 'production goroutines are forbidden in this synchronous library\n' >&2
	exit 1
fi
if rg -n '^type [[:alnum:]_]+ interface' --glob '*.go' \
	--glob '!**/*_test.go' --glob '!mathtest/**' .; then
	printf 'cross-family production interfaces are forbidden\n' >&2
	exit 1
fi
go list -deps ./... >/dev/null
