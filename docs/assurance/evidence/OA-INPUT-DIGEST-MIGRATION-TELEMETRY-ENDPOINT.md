# Telemetry Endpoint Input-Digest Migration

Reviewed at `2026-08-13T10:45:00Z` on `darwin/arm64` with Go `1.26.5`.

## Change Classification

Commit `81ae3383` corrected only the telemetry example bootstrap that translates
the standard URL-valued `OTEL_EXPORTER_OTLP_ENDPOINT` variable into the
explicit exporter configuration. It added validation tests and an internal
example configuration package. It did not change the public telemetry API,
the runtime behavior of consumers that construct `ExporterConfig` directly,
or the source of the reverse-dependent modules listed below.

The complete current `pkg/telemetry` gate passed after the correction,
including exact 100% production statement coverage and 259 of 259 viable
mutants killed. The operational campaigns that execute or package telemetry
were rerun separately and retain new observation timestamps.

## Reviewed Digest Transitions

The operational-assurance fingerprint includes transitive dependency source.
The telemetry example-only change therefore changed these reverse-dependent
fingerprints even though their own source and the behavior exercised by the
earlier evidence were unchanged:

| Module | Previous digest | Current digest |
| --- | --- | --- |
| `pkg/cloudevents/adapters/golib` | `841f6354d5eb2115787a1df411a216443e027c5732ab1c95e43569e905b87805` | `ef6e6e20715a9dc5b9a3acdcd7bfed4730954bc76e7c361cb317fe2c8ca662d3` |
| `pkg/queue-control-plane` | `0207f12c6344b55153ccfee41b4018b8b3e852bef2e78c97d334d18f319b8135` | `095a3b94cd0b133befe3a495d02efae7131b9fd93c8124e0aa8fd4737d17c84c` |
| `pkg/scheduler` | `84abed96d055f563be4569a7847821990440a412fd4c501bcbaa8e71c29bbbfd` | `40c60495dd99982e4c07fc0d4341c80cdc477ea078b2374740cf00a9875699c0` |
| `pkg/webhook` | `40f825729cb593a2b3d0fbdcd9807c31e453477d7c625bd939c54db604616788` | `023c58b1345692caecc5a47133070754a0ed820799f1a1483b846e02a7aa7d5e` |

## Claim Boundary

This record authorizes only the four exact one-way digest transitions above.
It does not migrate `pkg/telemetry` evidence, authorize any future digest,
or claim that the affected operational scenarios passed. Any mismatch outside
these exact transitions still fails closed.
