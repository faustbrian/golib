#!/usr/bin/env bash
set -euo pipefail

duration="${1:-2s}"
parallel="${FUZZ_PARALLEL:-2}"

case "${parallel}" in
  ''|*[!0-9]*)
    printf 'FUZZ_PARALLEL must be a positive integer\n' >&2
    exit 2
    ;;
esac
if [[ "${parallel}" -lt 1 ]]; then
  printf 'FUZZ_PARALLEL must be a positive integer\n' >&2
  exit 2
fi

for target in \
  FuzzRunArgv \
  FuzzCompileCommandGraph \
  FuzzTypedConversion \
  FuzzCompletionPartialArgv \
  FuzzLifecycleFailureAndCancellation \
  FuzzHelpMarkdownAndManifestGeneration \
  FuzzJSONErrorAndSuccessRendering \
  FuzzHumanTerminalRendering
do
  GOWORK=off go test . -run '^$' -parallel="${parallel}" \
    -fuzz="^${target}$" -fuzztime="${duration}"
done

GOWORK=off go test ./internal/engine -run '^$' -parallel="${parallel}" \
  -fuzz='^FuzzAdapterTranslation$' -fuzztime="${duration}"
