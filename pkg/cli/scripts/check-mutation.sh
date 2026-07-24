#!/usr/bin/env bash
set -euo pipefail

gremlins="${GREMLINS:-go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0}"

${gremlins} unleash --config .gremlins.yml
