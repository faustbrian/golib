#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
exec "${root}/scripts/run-modules.sh" mutation --modules \
  pkg/knapsack,pkg/knapsack/integration/references,pkg/knapsack/objective/gomoney
