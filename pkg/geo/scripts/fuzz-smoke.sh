#!/usr/bin/env bash
set -euo pipefail

duration="${FUZZ_TIME:-2s}"
targets=(
    ".|FuzzGeometryConstructors"
    ".|FuzzValueDecoding"
    "./geodesy|FuzzModels"
    "./adapter/gogeom|FuzzFromGoGeom"
    "./geojson|FuzzDecode"
    "./wkt|FuzzDecode"
    "./wkb|FuzzDecode"
    "./geohash|FuzzDecode"
    "./postgis|FuzzValueScan"
)

for target in "${targets[@]}"; do
    package="${target%%|*}"
    fuzz="${target#*|}"
    go test "$package" -run '^$' -fuzz "^${fuzz}$" -fuzztime "$duration"
done
