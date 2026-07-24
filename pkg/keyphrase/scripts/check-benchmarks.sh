#!/bin/sh
set -eu

go test -run '^$' -bench . -benchtime=1x ./...
