# Changelog

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Add opt-in consumption of the shared `resilience` budget with coordinated
  nested attempt lineage and bounded retry-plus-hedge amplification.
- Add an explicitly replay-safe, deadline-bounded hedged execution policy with
  fixed, scheduled, and dynamic delays.
- Add shared outstanding-work budgets, deterministic result selection,
  cooperative loser cancellation, explicit result disposal, cleanup waiting,
  and bounded lifecycle observations.
- Require budgets to declare a validated finite capacity and add pinned
  Failsafe-Go comparison benchmarks as a development-only dependency.
- Add explicit deterministic-scheduling, fault-path, clean-consumer, and
  supply-chain gates to the module contract.
- Replace the narrow mutation smoke check with the repository's complete viable
  mutant contract.

No migration is required because this is the first release.
