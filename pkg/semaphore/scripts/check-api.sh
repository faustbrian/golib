#!/bin/sh
set -eu

root="$(git rev-parse --show-toplevel)"
GOWORK=off "$root/scripts/check-api-baseline.sh" pkg/semaphore
