#!/bin/sh
set -eu
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env
# Gremlins derives every mutant timeout from its coverage-run duration. A cached
# baseline can make that budget shorter than an uncached mutant test process.
go clean -testcache
go run "github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}" unleash
