#!/usr/bin/env bash
set -euo pipefail

readonly image='maven@sha256:6fdc855a6ed81d288ca7ca37ac6ff5e9308b612485c0801d70b25a858c83d237'
readonly schema_id='00112233-4455-6677-8899-aabbccddeeff'
readonly payload='7061796c6f6164'
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work=$(mktemp -d)
maven_cache=$(mktemp -d)
jansi_tmp=$(mktemp -d)
cleanup() {
	find "$work" -depth -delete
	find "$maven_cache" -depth -delete
	find "$jansi_tmp" -depth -delete
}
trap cleanup EXIT

command -v docker >/dev/null || { echo 'docker is required for AWS Glue wire interoperability' >&2; exit 1; }
cp "$root/testdata/reference/pom.xml" "$root/testdata/reference/GlueWireReference.java" "$work/"
mkdir -p "$work/src/main/java"
mv "$work/GlueWireReference.java" "$work/src/main/java/"

official_output=$(docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges \
	--pids-limit 256 --memory 1g --tmpfs '/tmp:rw,nosuid,nodev,size=64m' \
	--user "$(id -u):$(id -g)" --env HOME=/tmp --env MAVEN_CONFIG=/maven \
	--env 'MAVEN_OPTS=-Djansi.tmpdir=/jansi -Djansi.force=false' \
	--volume "$work:/workspace:rw" --volume "$maven_cache:/maven:rw" \
	--volume "$jansi_tmp:/jansi:rw" \
	--workdir /workspace "$image" \
	mvn --quiet -Dmaven.repo.local=/maven compile exec:java \
	-Dexec.mainClass=GlueWireReference -Dexec.args="$schema_id $payload 100000")
official=$(printf '%s\n' "$official_output" | sed -n '1p')
official_benchmark=$(printf '%s\n' "$official_output" | sed -n '2p')
case "$official_benchmark" in
	'official_aws_java_serde ns/op='*' iterations=100000 payload_bytes=1024') ;;
	*) printf 'invalid AWS Java benchmark output: %s\n' "$official_benchmark" >&2; exit 1 ;;
esac
ours=$("$root/../../scripts/with-provider-gocache.sh" go run "$root/testdata/reference/go_frame.go" "$schema_id" "$payload")
test "$ours" = "$official" || {
	printf 'Go frame: %s\nAWS Java frame: %s\n' "$ours" "$official" >&2
	exit 1
}
printf 'AWS Glue Java SerDe v1.1.27 wire frame matched: %s\n' "$ours"
printf '%s\n' "$official_benchmark"
"$root/../../scripts/with-provider-gocache.sh" go test "$root" -run '^$' \
	-bench '^BenchmarkFrame$' -benchmem -benchtime=1s
