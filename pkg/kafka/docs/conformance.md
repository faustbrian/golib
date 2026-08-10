# Public conformance suites

The `kafkatest` package exposes consumer-facing conformance suites without
exporting franz-go types. They verify the stable Kafka policy contracts against
an explicitly selected broker fixture or provider implementation.
Normative ambiguities and package-selected interpretations are recorded in the
[specification decision register](specification-decisions.md).

## Broker harness

`BrokerHarness` keeps infrastructure mutation outside the production package.
The fixture supplies:

- an ordered broker list and the matching root `ClientSecurity` policy;
- isolated topic creation for a requested positive partition count;
- bounded direct-partition reads with explicit read-committed or
  read-uncommitted isolation; and
- authoritative consumer-group committed-offset lookup.

The direct reader returns owned `ConsumedRecord` values. It does not join a
group or mutate offsets. Topic creation is test-only infrastructure work; none
of the suites authorizes production topic, ACL, group, offset, or broker
configuration changes.

```go
func TestKafkaPolicy(t *testing.T) {
    harness := kafkatest.BrokerHarness{
        Brokers:         brokers,
        Security:        kafka.ClientSecurity{},
        NewTopic:        newIsolatedTopic,
        ReadRecords:     readExactPartitionRange,
        CommittedOffset: fetchStableCommittedOffset,
    }

    kafkatest.RunProducerConformance(t, harness)
    kafkatest.RunConsumerConformance(t, harness)
    kafkatest.RunTransactionConformance(t, harness)
    kafkatest.RunReplayConformance(t, harness)
    kafkatest.RunInspectorConformance(t, harness)
}
```

The broker-backed suites prove:

- synchronous, batch, and asynchronous production, input ownership, delivery
  metadata, explicit partitions, and post-close fencing;
- at-least-once record and batch handling across partial fetches, contiguous
  per-partition commits, failed-record and complete failed-batch redelivery,
  and retained consumed bytes;
- transaction commit and abort isolation, callback fencing, and atomic
  consume-transform-produce source-offset settlement;
- inclusive-start and exclusive-end replay, checkpoint resume, partition-local
  order, exact progress, single-use fencing, and group-offset independence; and
- cluster, topic, consumer-group lag, dependency, readiness, liveness, and
  post-close inspection behavior.

The suite does not infer support from a successful protocol connection. Record
the broker product, exact runtime version, image digest, deployment mode,
security profile, operating system, and architecture beside each execution
before using it as compatibility evidence.

## Authentication providers

`RunAuthenticationProviderConformance` accepts isolated factories for
`UsernamePasswordProvider`, `OAuthBearerProvider`,
`ClientCertificateProvider`, and `TrustAnchorProvider`. It proves cooperative
cancellation, independently owned refresh results, required expiry metadata,
and redacted formatting. It does not prove that a particular identity provider
or broker accepts the credentials; that requires a secured live-broker profile.

## Observers

`RunObserverConformance` exercises the root `ObserverPolicy` through its public
producer surface. It proves registration-copy ownership, synchronous order,
panic and error containment, cooperative timeout reporting, payload-free
metadata, and same-client reentrancy fencing. Observer failures remain
diagnostic and cannot replace the producer operation result.

## Repository gate

`make conformance` runs all seven named seams and the digest-pinned broker
fixture. The broader `make integration` command retains the multi-broker,
rebalance, authentication, replay-failure, and chaos evidence. A passing public
suite is necessary package-contract evidence; it is not a substitute for those
supported-version and failure-mode matrices.
