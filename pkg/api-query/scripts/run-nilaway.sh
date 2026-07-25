#!/bin/sh
set +e

printf '%s\n' 'Running advisory NilAway.'
go tool nilaway ./...
status=$?
if [ "$status" -ne 0 ]; then
	printf '%s\n' 'NilAway reported advisory findings; the gate remains non-blocking.'
else
	printf '%s\n' 'NilAway advisory completed without findings.'
fi
exit 0
