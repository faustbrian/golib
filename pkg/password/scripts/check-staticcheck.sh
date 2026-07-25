#!/bin/sh
set -eu
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env
go run "honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}" ./...
