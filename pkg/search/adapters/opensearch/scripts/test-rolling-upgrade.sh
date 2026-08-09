#!/bin/sh
set -eu

old_version=2.19.3
new_version=3.6.0
suffix=$$
network="golib-opensearch-upgrade-$suffix"
node1="golib-opensearch-upgrade-node1-$suffix"
node2="golib-opensearch-upgrade-node2-$suffix"
volume1="golib-opensearch-upgrade-data1-$suffix"
volume2="golib-opensearch-upgrade-data2-$suffix"
old_image="opensearchproject/opensearch:$old_version"
new_image="opensearchproject/opensearch:$new_version"
remove_old_image=${OPENSEARCH_CLEAN_IMAGES:-0}
remove_new_image=${OPENSEARCH_CLEAN_IMAGES:-0}

cleanup() {
	docker rm -f "$node1" "$node2" >/dev/null 2>&1 || true
	docker volume rm "$volume1" "$volume2" >/dev/null 2>&1 || true
	docker network rm "$network" >/dev/null 2>&1 || true
	if [ "$remove_old_image" -eq 1 ]; then docker image rm "$old_image" >/dev/null 2>&1 || true; fi
	if [ "$remove_new_image" -eq 1 ]; then docker image rm "$new_image" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT HUP INT TERM

if ! docker image inspect "$old_image" >/dev/null 2>&1; then remove_old_image=1; fi
if ! docker image inspect "$new_image" >/dev/null 2>&1; then remove_new_image=1; fi
docker network create "$network" >/dev/null
docker volume create "$volume1" >/dev/null
docker volume create "$volume2" >/dev/null

run_node() {
	name=$1
	node_name=$2
	volume=$3
	image=$4
	docker run -d --name "$name" --network "$network" -p 127.0.0.1::9200 \
		--cpus=1 --memory=1g --pids-limit=512 --ulimit nofile=65536:65536 \
		-v "$volume:/usr/share/opensearch/data" \
		-e cluster.name=golib-upgrade \
		-e "node.name=$node_name" \
		-e "discovery.seed_hosts=$node1,$node2" \
		-e "cluster.initial_cluster_manager_nodes=node1,node2" \
		-e DISABLE_SECURITY_PLUGIN=true \
		-e OPENSEARCH_JAVA_OPTS='-Xms512m -Xmx512m' \
		"$image" >/dev/null
}

wait_node() {
	container=$1
	port=$(docker port "$container" 9200/tcp | sed -n 's/.*://p')
	for _ in $(seq 1 180); do
		if curl --connect-timeout 2 --max-time 5 --fail --silent "http://127.0.0.1:$port/" >/dev/null; then
			printf '%s\n' "$port"
			return 0
		fi
		sleep 1
	done
	docker logs "$container" >&2
	return 1
}

wait_cluster() {
	port=$1
	expected_nodes=$2
	for _ in $(seq 1 180); do
		if curl --connect-timeout 2 --max-time 5 --fail --silent "http://127.0.0.1:$port/_cluster/health" | jq -e \
			--argjson nodes "$expected_nodes" \
			'.number_of_nodes == $nodes and .status != "red" and .initializing_shards == 0 and .relocating_shards == 0' \
			>/dev/null; then
			return 0
		fi
		sleep 1
	done
	printf 'cluster did not stabilize with %s nodes\n' "$expected_nodes" >&2
	return 1
}

run_node "$node1" node1 "$volume1" "$old_image"
run_node "$node2" node2 "$volume2" "$old_image"
port1=$(wait_node "$node1")
port2=$(wait_node "$node2")
wait_cluster "$port1" 2
urls="http://127.0.0.1:$port1,http://127.0.0.1:$port2"
OPENSEARCH_URLS="$urls" OPENSEARCH_EXPECTED_VERSION="$old_version" \
	go test -tags=integration -run TestRealOpenSearchMultiNodeRotation -count=1 .

docker stop "$node1" >/dev/null
OPENSEARCH_URLS="$urls" go test -tags=integration -run TestRealOpenSearchEndpointFailoverBudget -count=1 .
docker start "$node1" >/dev/null
port1=$(wait_node "$node1")
wait_cluster "$port1" 2

manager=$(curl --connect-timeout 2 --max-time 5 --fail --silent \
	"http://127.0.0.1:$port1/_cat/master?h=node" | tr -d '[:space:]')
case "$manager" in
node1)
	first_node=$node2
	first_name=node2
	first_volume=$volume2
	second_node=$node1
	second_name=node1
	second_volume=$volume1
	second_port=$port1
	;;
node2)
	first_node=$node1
	first_name=node1
	first_volume=$volume1
	second_node=$node2
	second_name=node2
	second_volume=$volume2
	second_port=$port2
	;;
*)
	printf 'unexpected cluster-manager node: %s\n' "$manager" >&2
	exit 1
	;;
esac

docker rm -f "$first_node" >/dev/null
run_node "$first_node" "$first_name" "$first_volume" "$new_image"
first_port=$(wait_node "$first_node")
wait_cluster "$second_port" 2
OPENSEARCH_URL="http://127.0.0.1:$second_port" OPENSEARCH_EXPECTED_VERSION="$old_version" \
	go test -tags=integration -run TestRealOpenSearchConformance -count=1 .

docker rm -f "$second_node" >/dev/null
run_node "$second_node" "$second_name" "$second_volume" "$new_image"
second_port=$(wait_node "$second_node")
wait_cluster "$second_port" 2
urls="http://127.0.0.1:$first_port,http://127.0.0.1:$second_port"
OPENSEARCH_URL="http://127.0.0.1:$second_port" OPENSEARCH_EXPECTED_VERSION="$new_version" \
	go test -tags=integration -run TestRealOpenSearchConformance -count=1 .
OPENSEARCH_URLS="$urls" OPENSEARCH_EXPECTED_VERSION="$new_version" \
	go test -tags=integration -run TestRealOpenSearchMultiNodeRotation -count=1 .
