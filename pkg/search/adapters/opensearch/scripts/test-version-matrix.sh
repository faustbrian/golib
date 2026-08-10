#!/bin/sh
set -eu

versions='2.19.6 3.8.0'
container=''
image=''
remove_image=0

cleanup() {
	if [ -n "$container" ]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
	fi
	if [ "$remove_image" -eq 1 ] && [ -n "$image" ]; then
		docker image rm "$image" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT HUP INT TERM

for version in $versions; do
	container="golib-opensearch-$version-$$"
	image="opensearchproject/opensearch:$version"
	remove_image=${OPENSEARCH_CLEAN_IMAGES:-0}
	if [ "${OPENSEARCH_KEEP_IMAGES:-0}" -eq 1 ]; then
		remove_image=0
	elif ! docker image inspect "$image" >/dev/null 2>&1; then
		remove_image=1
	fi
	docker run -d --name "$container" -p 127.0.0.1::9200 \
		--cpus=1 --memory=1g --pids-limit=512 --ulimit nofile=1024:1024 \
		-e discovery.type=single-node \
		-e DISABLE_SECURITY_PLUGIN=true \
		-e OPENSEARCH_JAVA_OPTS='-Xms512m -Xmx512m' \
		"$image" >/dev/null
	port="$(docker port "$container" 9200/tcp | sed -n 's/.*://p')"
	ready=0
	for _ in $(seq 1 120); do
		if curl --fail --silent "http://127.0.0.1:$port/" >/dev/null; then
			ready=1
			break
		fi
		sleep 1
	done
	if [ "$ready" -ne 1 ]; then
		docker logs "$container" >&2
		exit 1
	fi
	OPENSEARCH_URL="http://127.0.0.1:$port" \
	OPENSEARCH_EXPECTED_VERSION="$version" \
		go test -tags=integration -run 'TestRealOpenSearch(Conformance|BoundedLoad)' -count=1 .
	docker rm -f "$container" >/dev/null
	container=''
	if [ "$remove_image" -eq 1 ]; then
		docker image rm "$image" >/dev/null
	fi
	image=''
	remove_image=0
done
