# Equivalent Kafka mTLS and SASL/PLAIN capture, 2026-08-03

This directory publishes one bounded local comparison run. It is evidence for
the exact authenticated producer workloads and environment below, not a
general client ranking.

## Workloads

`BenchmarkEquivalentAuthenticatedSynchronousProduce` compares the package
policy, raw franz-go, and IBM/Sarama through two Apache Kafka 4.3.1 listeners:

- verified TLS 1.3 with a required client certificate; and
- SASL/PLAIN over verified TLS 1.3.

Every candidate uses idempotent all-ISR production, ordering-preserving
in-flight settings, Murmur2-keyed records, no compression, one partition, and
the same bounded delivery and retry policy. Each producer is warmed before the
timer. The matrix contains 128-byte and 1 KiB payloads, ten samples of ten
operations, 120 result samples, and 1,200 persistent deliveries.

`BenchmarkEquivalentAuthenticatedConnectProduceClose` measures construction,
one authenticated 128-byte keyed delivery, and bounded shutdown. Ten samples
of ten operations across both authentication modes and three clients contain
60 result samples and 600 complete connection lifecycles. Each result reports
exactly one connection lifecycle per operation.

The package policy obtains its immutable client certificate or PLAIN
credentials through its bounded provider contract. Raw franz-go and Sarama use
the same material directly. Runtime-generated private keys and passwords never
enter captured output. Topic creation, broker startup, fixture generation, and
the persistent-producer warmup remain outside the timer.

## Correctness boundary

Before capture,
`TestEquivalentAuthenticatedProducerOutcomes` passed under the race detector.
It asserted TLS 1.3 on both authenticated listeners and used an independent
authenticated consumer to prove every exact key and value produced by all
three clients. The fixture reported Apache Kafka 4.3.1 and OpenSSL 3.5.7 at
runtime.

The capture does not establish SCRAM, OAUTHBEARER, identity-provider,
credential refresh, rotation, authorization-denial, multi-broker, or failure
performance. Those remain separate correctness, compatibility, and fault
questions.

## Results

Persistent-delivery medians ranged from 2.712 to 8.742 milliseconds for the
policy mTLS path, 5.167 to 5.177 milliseconds for raw franz-go, and 11.27 to
14.06 milliseconds for Sarama. SASL/PLAIN medians ranged from 2.075 to 2.452
milliseconds for the policy, 3.075 to 4.278 milliseconds for raw franz-go, and
15.06 to 16.76 milliseconds for Sarama.

Complete mTLS lifecycle medians were 143.5 milliseconds for the policy, 76.23
milliseconds for raw franz-go, and 64.08 milliseconds for Sarama. Complete
SASL/PLAIN lifecycle medians were 121.0, 127.2, and 138.0 milliseconds,
respectively.

Persistent latency distributions spread as far as 243 percent and lifecycle
distributions as far as 94 percent on the shared local host. These results are
descriptive evidence for the exact workload and do not establish superiority
or a stable production regression budget.

## Environment and files

The host was Darwin arm64 on an Apple M4 Max with Go 1.26.5 and Docker Desktop
engine 29.6.2. The immutable Apache Kafka image is
`apache/kafka:4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837`.

- [`environment-authenticated.txt`](environment-authenticated.txt) binds the
  capture to the execution revision, toolchain, host, Docker engine, module
  versions, image digest, and every harness input hash.
- [`raw-authenticated.txt`](raw-authenticated.txt) contains every unmodified
  sample and the runtime broker assertion.
- [`authenticated-benchstat.txt`](authenticated-benchstat.txt) preserves
  latency, throughput, allocation, byte, and connection distributions.

## Commands

Every command used its own fresh isolated `GOCACHE`, inherited by all child
processes:

```sh
go test -race -tags=integration \
  -run '^TestEquivalent(TLS|Authenticated)ProducerOutcomes$' \
  -count=1 -timeout=8m ./...
make environment > environment-authenticated.txt
make auth-capture OUTPUT=raw-authenticated.txt
make analyze INPUT=raw-authenticated.txt > authenticated-benchstat.txt
```
