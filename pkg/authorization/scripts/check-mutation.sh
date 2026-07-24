#!/usr/bin/env bash
set -euo pipefail

version="v0.6.0"

go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash
