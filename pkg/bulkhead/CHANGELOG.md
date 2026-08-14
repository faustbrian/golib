# Changelog

All notable changes to this module are documented here.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Fixed-capacity and weighted process-local bulkheads with stable resource
  identity, immediate rejection, strict FIFO bounded waiting, typed terminal
  admission outcomes, and exactly-once owned permits.
- Generic context-aware execution with separate wait and execution timing,
  panic-safe release, detectable same-policy reentrancy, and honest behavior
  for callbacks that ignore cancellation.
- Bounded explicit partition registries, immutable policy revisions,
  synchronous failure-contained observations, snapshots, and graceful
  application-driven drain.
- Kubernetes sizing and shutdown guidance, resilience composition contracts,
  operations, migration, security, FAQ, hardening, fuzz, race, leak, mutation,
  compatibility, and comparative benchmark coverage.
- Adversarial terminal-path, weighted-starvation, concurrent partition
  replacement, Kubernetes lifecycle-model, and cross-package resilience
  composition campaigns, plus wait-latency, fairness, cancellation, observer,
  partition, throughput, and maintained-implementation benchmarks.
