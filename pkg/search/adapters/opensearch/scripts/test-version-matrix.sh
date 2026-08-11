#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/opensearch-images.env"
. "$script_dir/docker-test-ownership.sh"
adapter_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$adapter_dir"
root=$(git rev-parse --show-toplevel)

opensearch_run_id="$(date +%s)-$$-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
owner_label="$opensearch_owner_label_key=$opensearch_run_id"
benchmark_evidence_dir="${OPENSEARCH_BENCHMARK_EVIDENCE_DIR:-$root/.artifacts/pkg/search/adapters/opensearch/benchmark-$opensearch_run_id}"
mkdir -p "$benchmark_evidence_dir"

releases='old new'
container=''
image=''
remove_image=0
snapshot_dir=''
snapshot_container_path='/mnt/opensearch-snapshots'
old_benchmark=''
new_benchmark=''

{
	printf 'go=%s\n' "$(go version)"
	printf 'goos=%s\n' "$(go env GOOS)"
	printf 'goarch=%s\n' "$(go env GOARCH)"
	printf 'kernel=%s\n' "$(uname -srm)"
	printf 'opensearch_client=%s\n' "$(go list -m -f '{{.Version}}' github.com/opensearch-project/opensearch-go/v4)"
	printf 'benchstat=%s\n' "$(go list -m -f '{{.Version}}' golang.org/x/perf)"
	printf 'docker_server=%s\n' "$(docker version --format '{{.Server.Version}}')"
	printf 'old_opensearch=%s@%s\n' "$opensearch_old_version" "$opensearch_old_digest"
	printf 'new_opensearch=%s@%s\n' "$opensearch_new_version" "$opensearch_new_digest"
	printf 'benchmark_time=%s\n' "${INTEGRATION_BENCH_TIME:-20x}"
	printf 'benchmark_count=%s\n' "${INTEGRATION_BENCH_COUNT:-10}"
	printf 'container_limits=cpus:1,memory:1g,pids:512,nofile:1024,jvm_heap:384m\n'
	case "$(uname -s)" in
	Darwin)
		printf 'host_cpu=%s\n' "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || sysctl -n hw.model 2>/dev/null || printf unknown)"
		printf 'host_memory_bytes=%s\n' "$(sysctl -n hw.memsize 2>/dev/null || printf unknown)"
		;;
	Linux)
		printf 'host_cpu=%s\n' "$(sed -n 's/^model name[[:space:]]*:[[:space:]]*//p' /proc/cpuinfo | sed -n '1p')"
		printf 'host_memory=%s\n' "$(sed -n 's/^MemTotal:[[:space:]]*//p' /proc/meminfo | sed -n '1p')"
		;;
	*)
		printf 'host_cpu=unknown\nhost_memory=unknown\n'
		;;
	esac
} >"$benchmark_evidence_dir/environment.txt"

clear_snapshot_contents() {
	if [ -z "$snapshot_dir" ] || [ -z "$container" ] || ! opensearch_resource_owned container "$container"; then
		return
	fi
	if ! opensearch_resource_owned container "$container"; then
		printf 'refusing to clear snapshots through unowned container: %s\n' "$container" >&2
		return 1
	fi
	if [ "$snapshot_container_path" != '/mnt/opensearch-snapshots' ]; then
		printf 'refusing to clear unexpected container snapshot path: %s\n' "$snapshot_container_path" >&2
		exit 1
	fi
	docker exec "$container" sh -c '
		root=$1
		for path in "$root"/* "$root"/.[!.]* "$root"/..?*; do
			[ -e "$path" ] || continue
			rm -rf -- "$path"
		done
	' sh "$snapshot_container_path"
}

remove_snapshot_dir() {
	if [ -z "$snapshot_dir" ]; then
		return
	fi
	case "$(basename -- "$snapshot_dir")" in
	golib-opensearch-snapshot.*) ;;
	*)
		printf 'refusing to remove unexpected snapshot directory: %s\n' "$snapshot_dir" >&2
		exit 1
		;;
	esac
	find "$snapshot_dir" -depth -delete
	snapshot_dir=''
}

cleanup() {
	if [ -n "$container" ] && opensearch_resource_owned container "$container"; then
		clear_snapshot_contents
		opensearch_remove_owned container "$container"
	elif [ -n "$container" ]; then
		opensearch_remove_owned_if_present container "$container"
	fi
	if [ "$remove_image" -eq 1 ] && [ -n "$image" ]; then
		docker image rm "$image" >/dev/null 2>&1 || true
	fi
	remove_snapshot_dir
}
trap cleanup EXIT HUP INT TERM

for release in $releases; do
	case "$release" in
	old)
		version=$opensearch_old_version
		digest=$opensearch_old_digest
		;;
	new)
		version=$opensearch_new_version
		digest=$opensearch_new_digest
		;;
	esac
	container="golib-opensearch-$version-$opensearch_run_id"
	image="$opensearch_image_repository@$digest"
	snapshot_dir="${TMPDIR:-/tmp}/golib-opensearch-snapshot.$opensearch_run_id.$release"
	mkdir "$snapshot_dir"
	chmod 0777 "$snapshot_dir"
	remove_image=${OPENSEARCH_CLEAN_IMAGES:-0}
	if [ "${OPENSEARCH_KEEP_IMAGES:-0}" -eq 1 ]; then
		remove_image=0
	elif ! docker image inspect "$image" >/dev/null 2>&1; then
		remove_image=1
	fi
	docker run -d --name "$container" --label "$owner_label" -p 127.0.0.1::9200 \
		--cpus=1 --memory=1g --pids-limit=512 --ulimit nofile=1024:1024 \
		-e discovery.type=single-node \
		-e DISABLE_SECURITY_PLUGIN=true \
		-e OPENSEARCH_JAVA_OPTS='-Xms384m -Xmx384m' \
		-e "path.repo=$snapshot_container_path" \
		-v "$snapshot_dir:$snapshot_container_path" \
		"$image" >/dev/null
	port="$(docker port "$container" 9200/tcp | sed -n 's/.*://p')"
	ready=0
	for _ in $(seq 1 120); do
		if curl --connect-timeout 2 --max-time 5 --fail --silent "http://127.0.0.1:$port/" >/dev/null; then
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
	OPENSEARCH_SOAK_DURATION="${OPENSEARCH_SOAK_DURATION:-5s}" \
	OPENSEARCH_SNAPSHOT_REPOSITORY_PATH="$snapshot_container_path" \
			go test -tags=integration -run 'TestRealOpenSearch(Conformance|BoundedLoad|SnapshotRestore|DurableGuardSurvivesDeleteVersionGC|MixedApplicationProtocolVersions)' -count=1 .
	benchmark_file="$benchmark_evidence_dir/opensearch-$version.txt"
	if ! OPENSEARCH_URL="http://127.0.0.1:$port" \
		OPENSEARCH_EXPECTED_VERSION="$version" \
		go test -tags=integration -run '^$' -bench '^BenchmarkSharedSearchSemantics$' \
		-benchmem -benchtime="${INTEGRATION_BENCH_TIME:-20x}" \
		-count="${INTEGRATION_BENCH_COUNT:-10}" . >"$benchmark_file" 2>&1; then
		cat "$benchmark_file"
		exit 1
	fi
	if ! ./scripts/check-benchmark-evidence.sh "$benchmark_file" "${INTEGRATION_BENCH_COUNT:-10}"; then
		cat "$benchmark_file"
		exit 1
	fi
	cat "$benchmark_file"
	case "$release" in
	old) old_benchmark="$benchmark_file" ;;
	new) new_benchmark="$benchmark_file" ;;
	esac
	opensearch_assert_container_limits "$container"
	clear_snapshot_contents
	opensearch_remove_owned container "$container"
	container=''
	if [ "$remove_image" -eq 1 ]; then
		docker image rm "$image" >/dev/null
	fi
	image=''
	remove_image=0
	remove_snapshot_dir
done

go tool benchstat "$old_benchmark" "$new_benchmark" >"$benchmark_evidence_dir/benchstat.txt"
cat "$benchmark_evidence_dir/benchstat.txt"
printf 'benchmark evidence: %s\n' "$benchmark_evidence_dir"
