# Toxiproxy and infrastructure comparison

`fault-injection` operates at Go function and interface boundaries. It is fast,
seeded, directly attributable to a rule, and suitable for deterministic unit,
integration, race, fuzz, and resilience campaigns.

[Toxiproxy](https://github.com/Shopify/toxiproxy) operates as a TCP proxy and
models directional latency, timeouts, bandwidth, connection reset, sliced
traffic, and byte limits outside the process. It is the appropriate complement
when socket/proxy behavior, connection pools, protocol framing, or interactions
between real processes matter.

Neither an in-process adapter nor Toxiproxy is a Kubernetes fleet orchestrator.
Pod selection, disruption, rollout coordination, authorization, and blast
radius belong to an external system.

Do not compare in-process benchmark latency with proxy throughput as equivalent
fidelity. Choose the lowest boundary that observes the contract being tested,
and state what remains simulated.
