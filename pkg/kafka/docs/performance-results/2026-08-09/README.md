# Equivalent Kafka mTLS, PLAIN, SCRAM, and OAuth capture, 2026-08-09

This directory publishes one bounded local comparison run. It is evidence for
the exact authenticated producer workloads and environment below, not a
general client ranking.

## Workloads

`BenchmarkEquivalentAuthenticatedSynchronousProduce` compares the package
policy, raw franz-go, and IBM/Sarama through five Apache Kafka 4.3.1
authentication modes:

- verified TLS 1.3 with a required client certificate;
- SASL/PLAIN over verified TLS 1.3;
- SCRAM-SHA-256 over verified TLS 1.3;
- SCRAM-SHA-512 over verified TLS 1.3; and
- signed-JWT OAUTHBEARER over verified TLS 1.3.

Every candidate uses idempotent all-ISR production, ordering-preserving
in-flight settings, Murmur2-keyed records, no compression, one partition, and
the same bounded delivery and retry policy. Each producer is warmed before the
timer. The matrix contains 128-byte and 1 KiB payloads, ten samples of ten
operations, 300 result samples, and 3,000 persistent deliveries.

`BenchmarkEquivalentAuthenticatedConnectProduceClose` measures construction,
one authenticated 128-byte keyed delivery, and bounded shutdown. Each result
reports exactly one connection lifecycle per operation. Ten samples of ten
operations across five authentication modes and three clients contain 150
result samples and 1,500 complete connection lifecycles.

The package policy obtains its immutable client certificate, username and
password, or OAuth token through its bounded provider contract. Raw franz-go
and Sarama use the same material directly. Sarama's public SCRAM boundary uses
xdg-go/scram v1.2.0. The OAuth fixture uses Kafka's production validator with a
runtime-generated RS256 key, JWKS, issuer, and audience. Runtime-generated
private keys, passwords, and tokens never enter captured output. Topic
creation, broker startup, fixture generation, and the persistent-producer
warmup remain outside the timer.

## Correctness boundary

Before capture, `TestEquivalentAuthenticatedProducerOutcomes` passed under the
race detector. It asserted TLS 1.3 for every mode and used an independent
authenticated consumer to prove every exact key and value produced by all
three clients. The fixture reported Apache Kafka 4.3.1 and OpenSSL 3.5.7 at
runtime.

The capture does not establish external identity-provider, HTTP token
acquisition, HTTPS JWKS refresh, credential rotation, authorization-denial,
multi-broker, or failure performance. Those remain separate correctness,
compatibility, and fault questions.

## Results

Persistent SCRAM-SHA-256 medians ranged from 1.38 to 2.57 milliseconds for the
package policy, 1.81 to 2.93 milliseconds for raw franz-go, and 9.06 to 9.72
milliseconds for Sarama. Persistent SCRAM-SHA-512 medians ranged from 842
microseconds to 1.39 milliseconds for the policy, 780 microseconds to 2.50
milliseconds for raw franz-go, and 8.15 to 10.16 milliseconds for Sarama.
Persistent OAUTHBEARER medians ranged from 659 to 916 microseconds for the
policy, 742 microseconds to 1.40 milliseconds for raw franz-go, and 8.34 to
8.81 milliseconds for Sarama.

Complete SCRAM-SHA-256 connection, delivery, and shutdown medians were 49.28
milliseconds for the policy, 40.11 milliseconds for raw franz-go, and 32.92
milliseconds for Sarama. SCRAM-SHA-512 lifecycle medians were 46.60, 42.54, and
35.47 milliseconds, respectively. OAUTHBEARER lifecycle medians were 53.14,
63.49, and 40.66 milliseconds, respectively.

Distributions spread as far as 644 percent across the shared local fixture.
These results are descriptive evidence for the exact workload and do not
establish superiority or a stable production regression budget.

## Environment and files

The host was Darwin arm64 on an Apple M4 Max with Go 1.26.5 and Docker Desktop.
The immutable Apache Kafka image is
`apache/kafka:4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837`.

- `environment-authenticated.txt` binds the capture to the execution
  revision, toolchain, host, Docker engine, module versions, image digest, and
  every harness input hash.
- `raw-authenticated.txt` contains every unmodified sample and the runtime
  broker assertion.
- `authenticated-benchstat.txt` preserves latency, throughput, allocation,
  byte, and connection distributions.

## Commands

Every command used its own fresh isolated `GOCACHE`, inherited by all child
processes:

```sh
go test -race -tags=integration \
  -run '^TestEquivalentAuthenticatedProducerOutcomes$' \
  -count=1 -timeout=8m ./...
make environment
make auth-capture OUTPUT=raw-authenticated.txt
make analyze INPUT=raw-authenticated.txt
```
