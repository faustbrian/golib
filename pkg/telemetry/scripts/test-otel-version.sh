#!/bin/sh
set -eu

version=${1:?usage: test-otel-version.sh VERSION}

go get \
    "go.opentelemetry.io/otel@${version}" \
    "go.opentelemetry.io/otel/sdk@${version}" \
    "go.opentelemetry.io/otel/sdk/metric@${version}" \
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@${version}" \
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@${version}" \
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@${version}" \
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@${version}"

go mod tidy
go test ./...
