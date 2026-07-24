#!/bin/sh
set -eu

image=${KUBERNETES_BENCH_IMAGE:-golang:1.26.5-bookworm}

if ! command -v docker >/dev/null 2>&1; then
	printf '%s\n' 'docker is required for the Kubernetes benchmark gate' >&2
	exit 1
fi

docker run --rm \
	--cpus 2 \
	--memory 512m \
	--memory-swap 512m \
	-e GOFLAGS=-mod=readonly \
	-e GOMAXPROCS=2 \
	-v "$PWD:/src:ro" \
	-w /src \
	"$image" \
	sh -c '
		test "$(cat /sys/fs/cgroup/memory.max)" = 536870912
		test "$(cut -d " " -f 1 /sys/fs/cgroup/cpu.max)" = 200000
		go test -run "^$" -bench "^BenchmarkApproved" \
			-benchmem -benchtime=1x ./...
	'
