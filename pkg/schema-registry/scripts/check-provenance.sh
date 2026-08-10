#!/usr/bin/env bash
set -euo pipefail

readonly kafka='confluentinc/cp-kafka@sha256:acbbf674f2ed40e5d0a8ca51beb0f00692c866fc22b5ce06f8cadbdc54cd4436'
readonly confluent='confluentinc/cp-schema-registry@sha256:7ec0b15c6d5a64aa95b4201db5231ea58952b035869b95bed45624468ce10b34'
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
rg -F 'schema-registry-serde' providers/glue/testdata/reference/pom.xml >/dev/null
rg -F '<version>1.1.27</version>' providers/glue/testdata/reference/pom.xml >/dev/null
