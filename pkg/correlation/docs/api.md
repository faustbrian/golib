# API guide

Use `NewFactory` and `Start` for a workflow, `Next` for an outbound child, and
`Accept` for an inbound boundary. Use `NewCodec` only for custom carriers;
transport packages already configure their standard field names. `NewExternalID`
records non-correlation identifiers without promoting them.

Use `NewDeterministic` only after a privacy review. Use `Disclose` indirectly
through the log and telemetry packages unless implementing another bounded
observability adapter. The generated [API baseline](../api/baseline.txt) is the
authoritative exported-symbol inventory.
