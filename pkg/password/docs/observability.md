# Logging and telemetry

Use `password.WithObserver` to adapt events into standard `log/slog`,
`log`, or `telemetry`. The package intentionally depends on none of them.

Only record the supplied fields:

- `Operation`: `hash`, `verify`, or `verify_and_upgrade`;
- configured `Algorithm`;
- bounded `Outcome`;
- `NeedsRehash` boolean;
- `Duration` histogram value.

Do not attach password bytes, encoded hashes, salt/output data, usernames,
subjects, database IDs, raw errors, parser fields, or context-derived secrets.
Use operation/algorithm/outcome as bounded metric dimensions. Keep duration in
a histogram, and count resource rejection/cancellation separately.

Observers run synchronously and must bound latency. A panic is isolated, but a
blocking observer delays the password call. If an application needs asynchronous
delivery, it owns a bounded queue, drop policy, shutdown, and secret review.
