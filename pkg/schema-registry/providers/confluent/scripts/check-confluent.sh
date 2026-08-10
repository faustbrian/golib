#!/usr/bin/env bash
set -euo pipefail

readonly kafka_image='confluentinc/cp-kafka@sha256:acbbf674f2ed40e5d0a8ca51beb0f00692c866fc22b5ce06f8cadbdc54cd4436'
readonly registry_image='confluentinc/cp-schema-registry@sha256:7ec0b15c6d5a64aa95b4201db5231ea58952b035869b95bed45624468ce10b34'
readonly suffix="$$"
readonly network="schema-registry-integration-${suffix}"
readonly kafka="schema-registry-kafka-${suffix}"
readonly registry="schema-registry-confluent-${suffix}"
cleanup() {
	docker rm -f "$registry" "$kafka" >/dev/null 2>&1 || true
	docker network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT

command -v docker >/dev/null || { echo 'docker is required for Confluent integration' >&2; exit 1; }
docker info >/dev/null
docker network create "$network" >/dev/null

docker run --rm --detach --name "$kafka" --network "$network" \
	--env CLUSTER_ID='MkU3OEVBNTcwNTJENDM2Qk' \
	--env KAFKA_NODE_ID=1 \
	--env KAFKA_PROCESS_ROLES='broker,controller' \
	--env KAFKA_LISTENERS='PLAINTEXT://0.0.0.0:29092,CONTROLLER://0.0.0.0:29093' \
	--env KAFKA_ADVERTISED_LISTENERS="PLAINTEXT://${kafka}:29092" \
	--env KAFKA_LISTENER_SECURITY_PROTOCOL_MAP='CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT' \
	--env KAFKA_CONTROLLER_QUORUM_VOTERS="1@${kafka}:29093" \
	--env KAFKA_CONTROLLER_LISTENER_NAMES='CONTROLLER' \
	--env KAFKA_INTER_BROKER_LISTENER_NAME='PLAINTEXT' \
	--env KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
	--env KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS=0 \
	"$kafka_image" >/dev/null

docker run --rm --detach --name "$registry" --network "$network" \
	--publish 127.0.0.1::8081 \
	--env SCHEMA_REGISTRY_HOST_NAME="$registry" \
	--env SCHEMA_REGISTRY_LISTENERS='http://0.0.0.0:8081' \
	--env SCHEMA_REGISTRY_KAFKASTORE_BOOTSTRAP_SERVERS="PLAINTEXT://${kafka}:29092" \
	"$registry_image" >/dev/null

port="$(docker port "$registry" 8081/tcp | awk -F: 'NR == 1 {print $NF}')"
endpoint="http://127.0.0.1:${port}"
ready=false
for _ in {1..120}; do
	if curl --fail --silent --show-error "$endpoint/schemas/types" >/dev/null 2>&1; then
		ready=true
		break
	fi
	sleep 1
done
if [[ "$ready" != true ]]; then
	echo 'Confluent Schema Registry did not become ready' >&2
	docker logs "$registry" >&2
	exit 1
fi

SCHEMA_REGISTRY_CONFLUENT_INTEGRATION_ENDPOINT="$endpoint" \
	../../scripts/with-provider-gocache.sh go test -tags=integration . -count=1
