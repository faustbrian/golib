# Contributing

Changes must preserve the distinction between audit records and application
logs, traces, metrics, domain events, and event-sourcing history. Public API
changes require documentation, changelog entries, behavioral tests, exact
coverage, viable-mutant, race, fuzz, and clean-consumer evidence through the
repository gates.

Do not include credentials, real identities, production records, unrestricted
bodies, or secret-shaped values in fixtures, diagnostics, benchmarks, or
reports. PostgreSQL changes require migration, rollback, least-privilege,
backup/restore, retention, and supported-version integration evidence.
