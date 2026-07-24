#!/usr/bin/env bash
set -euo pipefail

readonly image='eclipse-temurin:25-jdk@sha256:201fbb8886b2d273218aa3a192f0afbf7b5ff65ee8cc6ef47f5dce2171f013ea'

command -v docker >/dev/null || {
  echo "docker is required for the pinned Java reference runtime" >&2
  exit 1
}

if [[ $# -lt 1 ]]; then
  echo "usage: $0 differential | benchmark <iterations>" >&2
  exit 1
fi

mode="$1"
shift
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
classes="$(mktemp -d "${TMPDIR:-/tmp}/xsd-java-classes.XXXXXX")"
trap 'rm -rf "$classes"' EXIT

container=(
  docker run --rm
  --network none
  --read-only
  --cap-drop ALL
  --security-opt no-new-privileges
  --pids-limit 256
  --memory 1g
  --tmpfs "/tmp:rw,nosuid,nodev,size=64m"
  --user "$(id -u):$(id -g)"
  --env HOME=/tmp
  --volume "$root:/workspace:ro"
  --volume "$classes:/classes:rw"
  --workdir /workspace
  "$image"
)

case "$mode" in
  differential)
    [[ $# -eq 0 ]] || {
      echo "differential mode does not accept arguments" >&2
      exit 1
    }
    "${container[@]}" sh -eu -c '
      javac -d /classes xsdtest/reference/ReferenceDifferential.java
      java -cp /classes xsdtest.reference.ReferenceDifferential \
        schema.xsd cases.tsv testdata/differential
    '
    ;;
  benchmark)
    [[ $# -eq 1 && "$1" =~ ^[1-9][0-9]*$ ]] || {
      echo "benchmark mode requires a positive iteration count" >&2
      exit 1
    }
    "${container[@]}" sh -eu -c "
      javac -d /classes xsdtest/reference/ReferenceBenchmark.java
      java -version 2>&1 | head -n 1
      java -cp /classes xsdtest.reference.ReferenceBenchmark \
        testdata/benchmark/schema.xsd \
        testdata/benchmark/valid.xml \
        testdata/benchmark/invalid.xml \
        \"\$1\"
    " sh "$1"
    ;;
  *)
    echo "unknown Java reference mode: $mode" >&2
    exit 1
    ;;
esac
