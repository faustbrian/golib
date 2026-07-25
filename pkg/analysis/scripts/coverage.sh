#!/bin/sh

set -eu

repository=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-coverage.XXXXXX")
coverage="$temporary/data"
binary="$temporary/analysis"

cleanup() {
	rm -rf "$temporary"
}
trap cleanup EXIT HUP INT TERM

mkdir "$coverage"

cd "$repository"
go test -covermode=count -coverpkg=./... ./... \
	-args -test.gocoverdir="$coverage"
go build -cover -covermode=count -coverpkg=./... -o "$binary" \
	./cmd/golib-analysis

GOCOVERDIR="$coverage" GOWORK=off "$binary" rules >/dev/null
GOCOVERDIR="$coverage" GOWORK=off "$binary" validate-config \
	testdata/coverage/advisory.yml >/dev/null
GOCOVERDIR="$coverage" GOWORK=off "$binary" check \
	-config testdata/coverage/advisory.yml ./... >/dev/null
GOCOVERDIR="$coverage" GOWORK=off "$binary" -V=full >/dev/null 2>&1

if GOCOVERDIR="$coverage" GOWORK=off "$binary" validate-config \
	testdata/coverage/invalid.yml >/dev/null 2>&1; then
	echo "validate-config unexpectedly accepted invalid policy" >&2
	exit 1
else
	status=$?
fi
if [ "$status" -ne 2 ]; then
	echo "validate-config exit status $status, want 2" >&2
	exit 1
fi

if GOCOVERDIR="$coverage" GOWORK=off "$binary" check \
	-config testdata/coverage/invalid.yml ./... >/dev/null 2>&1; then
	echo "check unexpectedly accepted invalid policy" >&2
	exit 1
else
	status=$?
fi
if [ "$status" -ne 2 ]; then
	echo "invalid check exit status $status, want 2" >&2
	exit 1
fi

if GOCOVERDIR="$coverage" GOWORK=off "$binary" check \
	-config testdata/coverage/blocking.yml ./... >/dev/null 2>&1; then
	echo "blocking check unexpectedly succeeded" >&2
	exit 1
else
	status=$?
fi
if [ "$status" -ne 1 ]; then
	echo "blocking check exit status $status, want 1" >&2
	exit 1
fi

result=$(go tool covdata percent -i="$coverage")
printf '%s\n' "$result"
if printf '%s\n' "$result" | awk '
	/github.com\/faustbrian\/golib\/pkg\/analysis/ &&
	$0 !~ /coverage: 100.0% of statements/ { failed = 1 }
	END { exit failed }
'; then
	:
else
	echo "production statement coverage is below 100%" >&2
	exit 1
fi

go tool covdata textfmt -i="$coverage" -o="$temporary/coverage.out"
total=$(go tool cover -func="$temporary/coverage.out" | tail -n 1)
printf '%s\n' "$total"
case "$total" in
	*"100.0%"*) ;;
	*)
		echo "aggregate production statement coverage is below 100%" >&2
		exit 1
		;;
esac
