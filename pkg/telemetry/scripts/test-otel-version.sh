#!/bin/sh
set -eu

version=${1:?usage: test-otel-version.sh VERSION}

for module in \
    go.opentelemetry.io/otel \
    go.opentelemetry.io/otel/metric \
    go.opentelemetry.io/otel/trace \
    go.opentelemetry.io/otel/sdk \
    go.opentelemetry.io/otel/sdk/metric \
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc \
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp \
    go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc \
    go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp
do
    go mod edit "-require=${module}@${version}"
done

go mod tidy
go test ./...
