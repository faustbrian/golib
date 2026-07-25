#!/usr/bin/env sh
set -eu

go test ./... -run '^$' -bench Benchmark -benchmem \
    -benchtime="${BENCH_TIME:-100ms}"

# Capture process peak resident memory for a representative encoded-image
# decode. GNU time reports KiB; BSD time reports bytes.
case "$(uname -s)" in
    Darwin)
        /usr/bin/time -l go test . -run '^$' \
            -bench '^BenchmarkDecodeEncodedQR$' -benchtime=1x >/dev/null
        ;;
    Linux)
        /usr/bin/time -v go test . -run '^$' \
            -bench '^BenchmarkDecodeEncodedQR$' -benchtime=1x >/dev/null
        ;;
    *)
        go test . -run '^$' -bench '^BenchmarkDecodeEncodedQR$' \
            -benchtime=1x >/dev/null
        ;;
esac
