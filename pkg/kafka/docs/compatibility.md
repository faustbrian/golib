# Compatibility and execution matrix

This matrix separates pinned implementation inputs from support claims. It was
recorded on 2026-07-26. Upstream protocol support is not package evidence.

## Pinned inputs

| Input | Exact version or identity | Verification |
| --- | --- | --- |
| Go toolchain and module language | Go 1.26.5, `go 1.26.5` | `go version`, `go env`, and `go.mod` |
| Host used for baseline | Darwin arm64, Apple M4 Max | Go environment and benchmark output |
| franz-go | v1.21.5, tag target `1ba5fd24f949a335dbc7caaef1d6037e132ef23e` | Go module proxy plus upstream tag; latest stable found on 2026-07-26 |
| kadm | v1.18.0, tag target `a7255a3f2bc7247e70a15b18080cc4e5cd1e42d6` | Go module proxy plus upstream tag; latest stable found on 2026-07-26 |
| testcontainers-go Kafka module | v0.43.0 | Go module proxy |
| Existing broker fixture | Confluent Local 7.5.0 digest `sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd` | Source-pinned single-node integration test; actual runtime version still needs assertion |
| Current Apache Kafka image selected for new evidence | `apache/kafka:4.3.1`, multi-platform index `sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837` | Docker registry manifest; arm64 manifest `sha256:c2b5172ab20d66381ec1729796a410fd611135821994526d4d42d2f256054af3` |
| Mutation tool | patched Gremlins v0.6.0 | 898 of 898 viable mutants killed; 100% efficacy and mutator coverage on 2026-07-26 |
| Lint/static analysis | golangci-lint v2.12.2, Staticcheck v0.7.0, NilAway `9fd1b8d7bac8` | Repository tool pins |
| Security/release tools | govulncheck v1.6.0, Gitleaks v8.30.1, go-licenses v2.0.1, CycloneDX v1.10.0 | Repository tool pins |
| OpenTelemetry semantic conventions | 1.43.0 | Selected current specification; adapter absent |
| MSK IAM Go signer | Not selected | Adapter absent; version must be pinned and reviewed before implementation |

Apache Kafka 4.3.1 was the latest supported Apache release found at execution
time. Selecting its image does not establish compatibility until the required
multi-broker matrix runs and asserts the broker version from the runtime.

## Current support status

| Dimension | Planned matrix | Current status |
| --- | --- | --- |
| Go | Minimum and current repository-supported releases | Only Go 1.26.5 on Darwin arm64 executed locally |
| Apache Kafka | Minimum reviewed release and current 4.3.1, KRaft, three brokers | Unverified; no Apache Kafka fixture currently executes |
| Confluent Platform/Local | Only explicitly exercised versions | One single-node 7.5.0 compatibility test; not a production support claim |
| Amazon MSK Provisioned | Selected Kafka versions, TLS/mTLS/SCRAM/IAM | Unverified |
| Amazon MSK Serverless | TLS and IAM with documented service limits | Unverified |
| Redpanda, Confluent Cloud, Event Hubs, other compatible services | Add only after direct testing | Unverified and unsupported |
| TLS and mTLS | TLS 1.2/1.3, hostname/root failures, rotation | Policy, cloning, rotation, and local TLS-handshake tests exist; no secured broker evidence |
| SASL/PLAIN | Verified TLS only | Policy and rotating-provider tests exist; broker compatibility unverified |
| SCRAM-SHA-256/512 | Verified TLS only | Policy and rotating-provider tests exist; broker compatibility unverified |
| OAUTHBEARER | Refreshing provider over verified TLS | Bounded expiring-provider policy tests exist; broker compatibility unverified |
| MSK IAM | Optional AWS signer adapter | Unimplemented |
| Producer | Single, batch, async, ordering, failure, shutdown | Policy APIs and deterministic tests exist; no real-broker batch, async, failure, or shutdown evidence yet |
| Consumer group | Classic cooperative/eager and reviewed next-generation protocol | Draft classic cooperative single-worker path only |
| Transactions | Produce and consume-transform-produce | Draft produce-only callback; no real-broker transaction evidence |
| Replay | Offset/timestamp planning, exact ranges, gaps, resume | Draft direct offset reader; no broker gap/truncation evidence |
| Inspection/health | Cluster/topic/group/durability and separated health signals | Partial metadata/lag/ping only |
| Operating systems/architectures | Linux amd64/arm64 plus repository-supported developer platforms | Local Darwin arm64 only; CI matrix not yet established |

## Primary sources

Design and implementation are checked against:

- [Apache Kafka documentation](https://kafka.apache.org/documentation/) and
  [supported downloads](https://kafka.apache.org/community/downloads/), plus
  the broker-compatible rules in Kafka's
  [`Topic` source](https://github.com/apache/kafka/blob/4.3.1/clients/src/main/java/org/apache/kafka/common/internals/Topic.java);
- [Apache Kafka producer configuration](https://kafka.apache.org/43/configuration/producer-configs/),
  [consumer configuration](https://kafka.apache.org/43/configuration/consumer-configs/),
  [consumer rebalance protocol](https://kafka.apache.org/43/operations/consumer-rebalance-protocol/),
  and [transaction protocol](https://kafka.apache.org/43/operations/transaction-protocol/);
- [franz-go v1.21.5 package documentation](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo@v1.21.5)
  and the tag-pinned upstream source;
- [Amazon MSK documentation](https://docs.aws.amazon.com/msk/), including
  [IAM client configuration](https://docs.aws.amazon.com/msk/latest/developerguide/configure-clients-for-iam-access-control.html);
- [OpenTelemetry Kafka semantic conventions](https://opentelemetry.io/docs/specs/semconv/messaging/kafka/);
  and
- [Go 1.26 release documentation](https://go.dev/doc/go1.26), the Go memory
  model, `context`, `crypto/tls`, fuzzing, race detector, and module docs.

The exact source revision or version used for an implemented capability must be
recorded beside its executable evidence, not inferred later from `HEAD`.
