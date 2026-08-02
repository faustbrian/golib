# Goal: Deterministic Fault Injection

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `fault-injection` as a deterministic, concurrency-safe test and controlled
experiment toolkit for exercising failure paths in Go libraries and services.
It MUST make every injected fault explicit, bounded, attributable,
reproducible, removable, and disabled by default.

It MUST NOT become a production chaos-control plane, Kubernetes operator, test
mocking framework, or hidden behavior switch.

## Authoritative References

- Failsafe-Go and goresilience failure-injection/chaos behavior;
- Toxiproxy and primary network fault-model documentation;
- Kubernetes pod lifecycle and disruption documentation;
- Go context, net/http, `net.Conn`, filesystem, time, random, race, fuzz, and
  testing documentation;
- this repository's security and resilience requirements.

## Fault Model

Support explicitly configured, composable rules for:

- returned sentinel or typed errors;
- latency before, during, or after an operation;
- context cancellation and deadline expiry;
- panic with caller-supplied safe value;
- dropped, truncated, duplicated, reordered, or corrupted byte operations where
  the adapter can define valid semantics;
- short reads/writes and temporary/permanent network failures;
- connection establishment, reset, half-close, and stream interruption;
- deterministic nth-call and bounded count schedules;
- seeded probability and deterministic sequence schedules; and
- caller predicates over bounded typed metadata.

Every rule MUST declare scope, activation, maximum injections, terminal
behavior, composition order, and observation. Probability alone MUST NOT be the
only reproducibility mechanism.

## Engine Contract

- Configuration is immutable and validated before use.
- No fault is active without an explicitly constructed injector.
- Rules have stable identities and deterministic precedence.
- Counters and seeded random streams are concurrency-safe with documented
  ordering semantics.
- Clocks, sleepers, and random sources are injectable.
- The engine has no unbounded goroutine, timer, history, key registry, or event
  queue.
- Reset and snapshot behavior is explicit; reset under active execution MUST be
  generation-safe.
- A disabled injector has measurable near-zero overhead and no allocations on
  hot paths where feasible.

## Adapters

Core adapters SHOULD include:

- generic function execution;
- `http.RoundTripper` and response-body faults;
- `net.Conn`, listener, dialer, reader, and writer boundaries;
- clock/sleeper/timer faults for resilience tests;
- filesystem interfaces where exact ownership is defined; and
- narrow database, cache, queue, Kafka, object-storage, and RPC adapters only
  when they do not force heavy dependencies into the root module.

Dependency-heavy adapters MUST be nested modules or downstream integrations.
Adapters MUST preserve interface contracts, ownership, partial-result
semantics, and cleanup.

Toxiproxy or infrastructure-level tools remain complementary for real network
behavior; this module MUST NOT claim kernel, proxy, broker, or cluster fidelity
from an in-process simulation.

## Safety Controls

- Production code MUST not enable faults from an environment variable alone.
- Any runtime experiment integration requires an explicit compiled/wired
  capability, authorization boundary, allowlist, expiry, rate/budget cap,
  audit event, and emergency disable.
- The core package SHOULD default to test-only construction patterns without
  relying on build tags as the sole safety boundary.
- Fault metadata MUST not contain request bodies, credentials, raw headers,
  database values, tenant identifiers, or arbitrary errors.
- Panics, corruption, and latency MUST remain bounded by caller policy.
- Injected behavior MUST be distinguishable from organic failure in diagnostics
  without changing the error contract under test unexpectedly.

## Kubernetes Semantics

In-process injectors affect one pod. Fleet experiments require an external
orchestrator that selects pods and owns blast radius; that orchestrator is out
of scope. Documentation MUST cover pod-local scope, replica selection bias,
rolling updates, ephemeral state, SIGTERM, readiness, and why HPA may react to
injected latency or errors.

The package MUST NOT coordinate experiments through pod gossip or claim a
fleet-wide percentage from independent per-pod random sources.

## Observability

Expose bounded events containing rule identity, adapter boundary, injection
kind, sequence, configured seed identity, activation generation, and safe
timing. Observers run outside internal locks and cannot veto an already selected
fault unless veto is an explicit deterministic rule stage.

## Documentation And Automation

Document deterministic test recipes, seeded schedules, adapters, partial IO,
cleanup, safety controls, Kubernetes caveats, Toxiproxy comparison, examples,
API, FAQ, security, operations, extension guidance, and changelog.

Require meaningful exact 100% statement coverage, exactly 100% viable mutation
kills, deterministic golden sequences, race, fuzz, stress, leak, adapter
contract, benchmark, API compatibility, docs, security, supply-chain, and
clean-consumer gates.

## Acceptance Criteria

- Identical configuration and execution ordering produce identical faults.
- Fault count, duration, state, memory, and diagnostics are bounded.
- Adapters preserve ownership and partial-operation contracts.
- Disabled/default use injects nothing and cannot be remotely activated.
- Pod-local versus fleet-wide scope is explicit.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
