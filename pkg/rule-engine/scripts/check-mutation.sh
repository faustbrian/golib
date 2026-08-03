#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
exec "${root}/scripts/run-modules.sh" mutation --modules \
  pkg/rule-engine,pkg/rule-engine/adapters/gomath,pkg/rule-engine/adapters/gomeasurement,pkg/rule-engine/adapters/gotemporal
