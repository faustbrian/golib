# Kafka Service Observer-Reentry Input-Digest Migration

Observed at `2026-08-19T15:33:08Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The `pkg/kafka/kafkaservice` overlapping-member interoperability scenario now
retries `Component.Stop` only when the underlying Kafka consumer reports its
documented transient `ErrObserverReentry` fence. The test shutdown callback is
retry-safe, as required by the public `Shutdown` callback contract. Every other
shutdown cause still fails immediately.

No production source, public API, Kafka protocol behavior, runtime
configuration, dependency, or service image changed. The
operational-assurance input digest therefore moves from
`f995cb45374a23e2d5f2e0101ed76921e837e58b0cac57f2b17cad0baf3cce7d` to
`3811b09b4ced2fb66263495d183a56db298b9c61abfff4f16ae27d1952a01672`.

## Behavioral Proof

The exact race-enabled overlapping-member shutdown scenario passed 20
consecutive repetitions. The complete pinned Apache Kafka 4.3.1
interoperability suite then passed. The strict `pkg/kafka/kafkaservice` module
contract passed every mandatory gate, including exact 100% statement coverage
and 113 of 113 viable mutants killed.

## Claim Boundary

This evidence authorizes only the exact one-way digest transition above for
`pkg/kafka/kafkaservice`. It preserves earlier operational observations without
relabelling their execution time, rerunning them, or extending their claims.
