#!/bin/sh
set -eu

if rg -n --glob '*.go' --glob '!**/*_test.go' \
    '(^|[^[:alnum:]_])(unsafe|SetTestNow)([^[:alnum:]_]|$)|go:linkname|import "C"' .; then
    echo "forbidden runtime or global-clock mechanism found" >&2
    exit 1
fi

if rg -n --glob '*.go' --glob '!**/*_test.go' 'time\.(Date|Parse|LoadLocation)' .; then
    echo "calendar or timezone behavior does not belong in clock" >&2
    exit 1
fi
