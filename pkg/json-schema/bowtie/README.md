# Bowtie interoperability

The harness implements Bowtie protocol version 1 over newline-delimited JSON
on standard input and output. It advertises all six released dialects, accepts
implicit-dialect changes, preserves arbitrary JSON sequence identifiers, and
uses each case's registry through the in-memory resource loader.

Build the pinned, network-disabled runtime image from this module directory:

```sh
docker build -f bowtie/Dockerfile -t localhost/json-schema-bowtie .
```

Run Bowtie against any supported dialect, for example:

```sh
bowtie run \
  --dialect https://json-schema.org/draft/2020-12/schema \
  -i localhost/json-schema-bowtie
```

`make bowtie-smoke` validates start, dialect, registry-backed run, caught
error, and stop behavior without Docker or a Bowtie installation. The harness
returns caught protocol errors and has no skip allowlist.

The external interoperability gate uses `bowtie-json-schema==2026.6.1` with
protocol-response validation. Each released dialect runs from the pinned local
official-suite checkout, and CI retains the raw Bowtie JSON report as an
artifact for independent summary or failure inspection.

`make bowtie-report` regenerates the checked-in raw reports, statistics, and
SHA-256 manifest under `bowtie/reports`. It requires Docker, `uvx`, and `jq`.
