# FAQ

## Why not use a mock?

Use a mock or small fake when one isolated return value is the contract. Use
this module when deterministic scheduling, composition, attribution, partial
IO, ownership, cleanup, or concurrent accounting is part of the test.

## Can an environment variable enable faults?

No. The package never reads the environment. An injector must be explicitly
constructed and wired.

## Is seeded probability deterministic under concurrency?

It is deterministic for the same eligible-call ordering. Mutex acquisition
defines that ordering. Record event sequences for replay; goroutine launch
order is insufficient.

## Does reset cancel in-flight faults?

No. Reset starts a new generation and clears current counters. Decisions
already selected remain attributable to their original generation.

## Are observers ordered?

Each selecting goroutine calls its observer synchronously after releasing the
engine lock. Concurrent delivery may interleave. Use generation and sequence
for ordering.

## Does the network adapter simulate TCP exactly?

No. It preserves Go interface contracts while simulating failures at an
in-process boundary. Use Toxiproxy or infrastructure tests for real proxy,
socket, kernel, broker, or cluster behavior.

## Can every pod use the same seed for a fleet percentage?

No. That creates independent local streams whose traffic and lifetimes differ.
An external orchestrator must select pods and own fleet blast radius.

## Where do Kafka or database adapters belong?

In nested modules or downstream integrations that can depend on the actual
client and prove its protocol-specific error and ownership contracts.
