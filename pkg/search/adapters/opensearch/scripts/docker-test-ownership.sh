#!/bin/sh

opensearch_owner_label_key='com.faustbrian.golib.opensearch.run'

opensearch_resource_owned() {
	type=$1
	name=$2
	case "$type" in
	container)
		actual=$(docker inspect --format "{{ index .Config.Labels \"$opensearch_owner_label_key\" }}" "$name" 2>/dev/null || true)
		;;
	network)
		actual=$(docker network inspect --format "{{ index .Labels \"$opensearch_owner_label_key\" }}" "$name" 2>/dev/null || true)
		;;
	volume)
		actual=$(docker volume inspect --format "{{ index .Labels \"$opensearch_owner_label_key\" }}" "$name" 2>/dev/null || true)
		;;
	*)
		return 1
		;;
	esac
	[ "$actual" = "$opensearch_run_id" ]
}

opensearch_resource_exists() {
	type=$1
	name=$2
	case "$type" in
	container) docker inspect "$name" >/dev/null 2>&1 ;;
	network) docker network inspect "$name" >/dev/null 2>&1 ;;
	volume) docker volume inspect "$name" >/dev/null 2>&1 ;;
	*) return 1 ;;
	esac
}

opensearch_remove_owned() {
	type=$1
	name=$2
	if ! opensearch_resource_owned "$type" "$name"; then
		printf 'refusing to remove unowned %s: %s\n' "$type" "$name" >&2
		return 1
	fi
	case "$type" in
	container) docker rm -f "$name" >/dev/null ;;
	network) docker network rm "$name" >/dev/null ;;
	volume) docker volume rm "$name" >/dev/null ;;
	esac
}

opensearch_remove_owned_if_present() {
	type=$1
	name=$2
	if ! opensearch_resource_exists "$type" "$name"; then
		return 0
	fi
	opensearch_remove_owned "$type" "$name"
}

opensearch_assert_container_limits() {
	name=$1
	expected_nofile=${2:-1024}
	case "$expected_nofile" in
	1024 | 65536) ;;
	*)
		printf 'unsupported expected file-descriptor limit: %s\n' "$expected_nofile" >&2
		return 1
		;;
	esac
	if ! opensearch_resource_owned container "$name"; then
		printf 'container is not owned by this run: %s\n' "$name" >&2
		return 1
	fi
	docker inspect "$name" | jq -e --argjson nofile "$expected_nofile" '
		.[0].HostConfig.NanoCpus == 1000000000 and
		.[0].HostConfig.Memory == 1073741824 and
		.[0].HostConfig.PidsLimit == 512 and
		(any(.[0].HostConfig.Ulimits[];
			.Name == "nofile" and .Soft == $nofile and .Hard == $nofile)) and
		(.[0].State.OOMKilled | not)
	' >/dev/null
	fd_count=$(docker exec "$name" sh -c 'set -- /proc/1/fd/*; printf "%s\n" "$#"')
	if [ "$fd_count" -gt "$expected_nofile" ]; then
		printf 'container file-descriptor use exceeds limit: %s\n' "$fd_count" >&2
		return 1
	fi
	stats=$(docker stats --no-stream --format '{{.CPUPerc}} {{.MemPerc}}' "$name")
	printf '%s\n' "$stats" | awk '
		{
			gsub(/%/, "", $1)
			gsub(/%/, "", $2)
			if (($1 + 0) > 105 || ($2 + 0) > 95) exit 1
		}
	' || {
		printf 'container resource utilization exceeded CPU or memory bound: %s\n' "$stats" >&2
		return 1
	}
}
