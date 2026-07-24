#!/bin/sh
set -eu

destination=${1:-}
case "$destination" in
    docs/benchmarks/*.txt) ;;
    *)
        echo 'usage: capture-benchmark.sh docs/benchmarks/NAME.txt' >&2
        exit 2
        ;;
esac

directory=$(dirname -- "$destination")
mkdir -p -- "$directory"
temporary=$(mktemp "$directory/.benchmark-XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM

if ! git diff --quiet -- . || ! git diff --cached --quiet -- .; then
    echo 'commit tracked openapi changes before capturing evidence' >&2
    exit 1
fi

export LC_ALL=C
export TZ=UTC
export GOMAXPROCS=1

{
    echo '# openapi benchmark evidence'
    echo "# revision: $(git rev-parse HEAD)"
    echo "# captured: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    echo '# command: go test . -run ^$ -bench . -benchmem -benchtime=250ms -count=3 -cpu=1'
    go version
    uname -mrs
    go test . -run '^$' -bench . -benchmem -benchtime=250ms -count=3 -cpu=1
    echo '# peak process memory probe'
    case "$(uname -s)" in
        Darwin)
            /usr/bin/time -l go test . -run '^$' -bench . -benchmem \
                -benchtime=1x -count=1 -cpu=1 2>&1
            ;;
        Linux)
            /usr/bin/time -v go test . -run '^$' -bench . -benchmem \
                -benchtime=1x -count=1 -cpu=1 2>&1
            ;;
        *)
            echo 'peak process memory probe unsupported on this platform'
            ;;
    esac
} >"$temporary"

chmod 0644 "$temporary"
mv -- "$temporary" "$destination"
trap - EXIT HUP INT TERM
