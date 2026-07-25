#!/usr/bin/env bash
set -uo pipefail

go run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260710181136-2378218750e4 \
	-include-pkgs='github.com/faustbrian/golib/pkg/calendar' ./...
status=$?
if [[ $status -ne 0 ]]; then
	printf 'NilAway advisory findings reported with status %d\n' "$status" >&2
fi
exit 0
