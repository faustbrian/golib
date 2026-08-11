#!/usr/bin/env bash
set -euo pipefail

readonly kafka='confluentinc/cp-kafka@sha256:0ad069035863aa1b090f4d9af47bfd2c08dc32864f3575d7d8579e3155c2586d'
readonly confluent='confluentinc/cp-schema-registry@sha256:f0cfd047a839c1ace54d93b92e3459f0d03dc3b5c9db1192a2246fd79b4f44c4'
readonly maven='maven@sha256:6fdc855a6ed81d288ca7ca37ac6ff5e9308b612485c0801d70b25a858c83d237'

command -v docker >/dev/null || { echo 'docker is required for provenance verification' >&2; exit 1; }
for image in "$kafka" "$confluent" "$maven"; do
	digest=${image##*@}
	manifest=$(docker buildx imagetools inspect "$image" --format '{{json .Manifest}}')
	printf '%s' "$manifest" | rg -F "$digest" >/dev/null || {
		echo "remote image digest does not match ${image}" >&2
		exit 1
	}
done

aws_commit=$(git ls-remote https://github.com/awslabs/aws-glue-schema-registry.git refs/tags/v1.1.27 | awk 'NR == 1 {print $1}')
test "$aws_commit" = 'b280404e615b4e63e2fb33b1aedc228e039fbf31'
franz_commit=$(git ls-remote https://github.com/twmb/franz-go.git 'refs/tags/pkg/sr/v1.8.0^{}' | awk 'NR == 1 {print $1}')
test "$franz_commit" = 'f9ccda5bd05883e50d9885ecff0c45f509efb045'

rg -F "$kafka" providers/confluent/scripts/check-confluent.sh >/dev/null
rg -F "$confluent" providers/confluent/scripts/check-confluent.sh >/dev/null
rg -F "$maven" providers/glue/scripts/check-wire-reference.sh >/dev/null
rg -F "$maven" providers/confluent/scripts/check-wire-reference.sh >/dev/null
rg -F 'kafka-schema-serializer' providers/confluent/testdata/reference/pom.xml >/dev/null
rg -F '<version>8.3.1</version>' providers/confluent/testdata/reference/pom.xml >/dev/null
rg -F 'b9cb84802804bb7feff2bc998b386ccd3e6b1a2342d168b21e792cfb62dabdcb' \
	providers/confluent/scripts/check-wire-reference.sh >/dev/null
rg -F 'cc7a225a7ae24ad4b62488e7c75480a2a9470e4282a83281934b9a2e3ea196d1' \
	providers/confluent/scripts/check-wire-reference.sh >/dev/null
rg -F 'schema-registry-serde' providers/glue/testdata/reference/pom.xml >/dev/null
rg -F '<version>1.1.27</version>' providers/glue/testdata/reference/pom.xml >/dev/null
