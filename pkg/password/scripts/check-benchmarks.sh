#!/bin/sh
set -eu

go test -run '^$' -bench '^BenchmarkApproved' -benchmem -benchtime=1x ./...
