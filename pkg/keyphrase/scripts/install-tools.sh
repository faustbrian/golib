#!/bin/sh
set -eu

. ./tools/versions.env
go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
go install "honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
go install "github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}"
go install "github.com/google/go-licenses@${GO_LICENSES_VERSION}"
