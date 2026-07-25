#!/bin/sh
set -eu

profile="$(mktemp "${TMPDIR:-/tmp}/money-coverage.XXXXXX")"
trap 'rm -f "$profile"' EXIT

go test -count=1 -covermode=atomic -coverpkg=.,./encoding,./format \
    -coverprofile="$profile" . ./encoding ./format

awk '
    BEGIN {
        expected["github.com/faustbrian/golib/pkg/money"] = 1
        expected["github.com/faustbrian/golib/pkg/money/encoding"] = 1
        expected["github.com/faustbrian/golib/pkg/money/format"] = 1
    }
    FNR == 1 { next }
    {
        split($1, location, ":")
        package = location[1]
        sub("/[^/]+$", "", package)
        block = $1
        block_package[block] = package
        block_statements[block] = $(NF - 1) + 0
        if (($NF + 0) > 0) block_covered[block] = 1
    }
    END {
        failed = 0
        for (block in block_statements) {
            package = block_package[block]
            total[package] += block_statements[block]
            if (block_covered[block]) covered[package] += block_statements[block]
        }
        for (package in expected) {
            printf "%s %d/%d statements\n", package, covered[package], total[package]
            if (total[package] == 0 || covered[package] != total[package]) failed = 1
        }
        exit failed
    }
' "$profile" | sort
