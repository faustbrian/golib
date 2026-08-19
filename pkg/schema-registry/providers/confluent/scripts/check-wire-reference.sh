#!/usr/bin/env bash
set -euo pipefail

readonly image='maven@sha256:6fdc855a6ed81d288ca7ca37ac6ff5e9308b612485c0801d70b25a858c83d237'
readonly artifact_sha256='b9cb84802804bb7feff2bc998b386ccd3e6b1a2342d168b21e792cfb62dabdcb'
readonly artifact_pom_sha256='cc7a225a7ae24ad4b62488e7c75480a2a9470e4282a83281934b9a2e3ea196d1'
readonly schema_id='17'
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

command -v docker >/dev/null || { echo 'docker is required for Confluent wire interoperability' >&2; exit 1; }
cp "$root/testdata/reference/pom.xml" "$root/testdata/reference/ConfluentWireReference.java" "$work/"
mkdir -p "$work/src/main/java"
mv "$work/ConfluentWireReference.java" "$work/src/main/java/"

official_output=$(docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges \
	--pids-limit 256 --memory 1g --tmpfs '/tmp:rw,nosuid,nodev,size=64m' \
	--user "$(id -u):$(id -g)" --env HOME=/tmp --env MAVEN_CONFIG=/maven \
	--env 'MAVEN_OPTS=-Djansi.tmpdir=/jansi -Djansi.force=false' \
	--volume "$work:/workspace:rw" --volume "$maven_cache:/maven:rw" \
	--volume "$jansi_tmp:/jansi:rw" \
	--workdir /workspace "$image" \
	mvn --quiet -Dmaven.repo.local=/maven compile exec:java \
	-Dexec.mainClass=ConfluentWireReference -Dexec.args="$schema_id $payload 100000")
official_classic=$(printf '%s\n' "$official_output" | sed -n '1p')
official_protobuf=$(printf '%s\n' "$official_output" | sed -n '2p')
official_classic_benchmark=$(printf '%s\n' "$official_output" | sed -n '3p')
official_protobuf_benchmark=$(printf '%s\n' "$official_output" | sed -n '4p')
case "$official_classic_benchmark" in
	'official_confluent_java_schema_id_serializer_classic ns/op='*' iterations=100000 payload_bytes=1024 observed='*) ;;
	*) printf 'invalid Confluent Java classic benchmark output: %s\n' "$official_classic_benchmark" >&2; exit 1 ;;
esac
case "$official_protobuf_benchmark" in
	'official_confluent_java_schema_id_serializer_protobuf ns/op='*' iterations=100000 payload_bytes=1024 observed='*) ;;
	*) printf 'invalid Confluent Java Protobuf benchmark output: %s\n' "$official_protobuf_benchmark" >&2; exit 1 ;;
esac
artifact="$maven_cache/io/confluent/kafka-schema-serializer/8.3.1/kafka-schema-serializer-8.3.1.jar"
artifact_pom="$maven_cache/io/confluent/kafka-schema-serializer/8.3.1/kafka-schema-serializer-8.3.1.pom"
test -f "$artifact"
test -f "$artifact_pom"
test "$(shasum -a 256 "$artifact" | awk '{print $1}')" = "$artifact_sha256"
test "$(shasum -a 256 "$artifact_pom" | awk '{print $1}')" = "$artifact_pom_sha256"
grep -F -- '<name>Apache License 2.0</name>' "$artifact_pom" >/dev/null

ours=$("$root/../../scripts/with-provider-gocache.sh" go run "$root/testdata/reference/go_frame.go" "$schema_id" "$payload")
ours_classic=$(printf '%s\n' "$ours" | sed -n '1p')
ours_protobuf=$(printf '%s\n' "$ours" | sed -n '2p')
test "$ours_classic" = "$official_classic" || {
	printf 'Go classic frame: %s\nConfluent Java frame: %s\n' "$ours_classic" "$official_classic" >&2
	exit 1
}
test "$ours_protobuf" = "$official_protobuf" || {
	printf 'Go Protobuf frame: %s\nConfluent Java frame: %s\n' "$ours_protobuf" "$official_protobuf" >&2
	exit 1
}
printf 'Confluent Java 8.3.1 classic frame matched: %s\n' "$ours_classic"
printf 'Confluent Java 8.3.1 Protobuf frame matched: %s\n' "$ours_protobuf"
printf '%s\n%s\n' "$official_classic_benchmark" "$official_protobuf_benchmark"
"$root/../../scripts/with-provider-gocache.sh" go test "$root" -run '^$' \
	-bench '^(BenchmarkClassicFrame|BenchmarkProtobufFrame)$' -benchmem -benchtime=1s
