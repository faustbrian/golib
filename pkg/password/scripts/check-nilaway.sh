#!/bin/sh
set -u
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env

go run "go.uber.org/nilaway/cmd/nilaway@${NILAWAY_VERSION}" ./...
status=$?
if [ "$status" -ne 0 ]; then
	printf 'NilAway advisory findings exited with status %d\n' "$status" >&2
fi
exit 0
