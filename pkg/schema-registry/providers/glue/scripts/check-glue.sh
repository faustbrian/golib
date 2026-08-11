#!/usr/bin/env bash
set -euo pipefail

../../scripts/with-provider-gocache.sh go test -tags=integration . -count=1
