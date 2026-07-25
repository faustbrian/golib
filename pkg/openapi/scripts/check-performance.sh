#!/bin/sh
set -eu

report=$(mktemp)
trap 'rm -f "$report"' EXIT HUP INT TERM

GOMAXPROCS=1 go test . -run '^$' -bench . -benchmem \
    -benchtime=100ms -count=1 -cpu=1 >"$report"

if ! awk '
BEGIN {
    limit["BenchmarkParseJSON"] = 13000
    limit["BenchmarkParseJSONScaling/paths_1"] = 330
    limit["BenchmarkParseJSONScaling/paths_100"] = 13000
    limit["BenchmarkParseJSONScaling/paths_1000"] = 125000
    limit["BenchmarkParseInvalidJSON"] = 13000
    limit["BenchmarkParseRejectedDepth"] = 30
    limit["BenchmarkValidateDocument"] = 285000
    limit["BenchmarkValidateDocumentCold"] = 312000
    limit["BenchmarkValidateSchemaHeavyDocument"] = 93000
    limit["BenchmarkSerializeJSON"] = 7100
    limit["BenchmarkResolveInternalReference"] = 13
    limit["BenchmarkResolveFileResource"] = 125
    limit["BenchmarkResolveHTTPResource"] = 165
    limit["BenchmarkBundleComponents"] = 58000
    limit["BenchmarkDereferenceObjects"] = 12500
    limit["BenchmarkDereferenceCycle"] = 330
    limit["BenchmarkOperationDiff"] = 16000
    limit["BenchmarkFilterOperations"] = 2200
    limit["BenchmarkMergeDocuments"] = 1100
    limit["BenchmarkConvertOpenAPI31To32"] = 12
    limit["BenchmarkConvertOpenAPI30To31"] = 14000
    limit["BenchmarkConvertSwagger20ToOpenAPI30"] = 7300
    limit["BenchmarkConvertOpenAPI31To30"] = 14500
    limit["BenchmarkConvertOpenAPI32To31"] = 9300
    limit["BenchmarkConvertOpenAPI32ToSwagger20"] = 33000
}
/^Benchmark/ {
    name = $1
    sub(/-[0-9]+$/, "", name)
    if (name in limit) {
        allocations = $(NF - 1) + 0
        seen[name]++
        if (allocations > limit[name]) {
            printf "%s allocations %d exceed budget %d\n", \
                name, allocations, limit[name] > "/dev/stderr"
            failed = 1
        }
    }
}
END {
    for (name in limit) {
        if (!seen[name]) {
            printf "missing benchmark evidence for %s\n", name > "/dev/stderr"
            failed = 1
        }
    }
    exit failed
}
' "$report"; then
    cat "$report" >&2
    exit 1
fi

echo 'performance allocation budgets passed'
