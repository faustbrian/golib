#!/bin/sh
set -eu

go mod verify
go mod tidy -diff
go list -deps ./... >/dev/null
