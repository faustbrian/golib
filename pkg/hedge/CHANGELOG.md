# Changelog

## Unreleased

### Added

- Add an explicitly replay-safe, deadline-bounded hedged execution policy with
  fixed, scheduled, and dynamic delays.
- Add shared outstanding-work budgets, deterministic result selection,
  cooperative loser cancellation, explicit result disposal, cleanup waiting,
  and bounded lifecycle observations.
- Require budgets to declare a validated finite capacity and add pinned
  Failsafe-Go comparison benchmarks as a development-only dependency.

No migration is required because this is the first release.
