# Compatibility

The module targets Go 1.26.5. Public API compatibility is checked against
`api/baseline.txt`. The only runtime dependencies are `identifier` for
secure default generation and OpenTelemetry trace contracts for optional
links.

The aggregate repository uses a relative module replacement for
`identifier`. A standalone publication must replace that with a released
module version before tagging. Transport wire keys are stable but remain
configurable for controlled migrations.
