# audit

`audit` is an infrastructure-neutral Go library for immutable-by-contract,
security-relevant and business-relevant records. It records who did what, to
which stable resource, when, from where, and with what outcome.

Audit records are deliberately separate from logs, traces, metrics, domain
events, and event-sourcing history. Using an event store does not make that
store a compliant audit trail, and using this library does not by itself
establish legal or regulatory compliance.

## Packages

- `github.com/faustbrian/golib/pkg/audit`: records, validation, delivery policy,
  privacy, querying, export, integrity, retention, and safe observation hooks.
- `github.com/faustbrian/golib/pkg/audit/memory`: bounded process-local adapter
  for tests; it is not durable.
- `github.com/faustbrian/golib/pkg/audit/postgres`: separately releasable
  PostgreSQL durable adapter and caller-owned transaction writer.

## Quick start

```go
builder, err := audit.NewBuilder(audit.BuilderConfig{})
if err != nil {
    return err
}
record, err := builder.Build(audit.RecordInput{
    OccurredAt: time.Now(),
    Action:     "invoice.approved",
    Outcome:    audit.OutcomeSucceeded,
    Actor:      audit.ActorInput{Kind: audit.ActorHuman, ID: userID},
    Subject:    audit.SubjectInput{Type: "invoice", ID: invoiceID},
    Context:    audit.ContextInput{TenantID: tenantID},
    Changes:    audit.ChangeSetInput{NoChange: true},
})
if err != nil {
    return err
}
redactor, err := audit.NewRedactor(audit.RedactionRules{})
if err != nil {
    return err
}
recorder, err := audit.NewRecorder(audit.RecorderConfig{
    Sink: durableSink, Redactor: redactor, Mode: audit.DeliveryFailClosed,
})
if err != nil {
    return err
}
_, err = recorder.Submit(ctx, record)
return err
```

The caller must select fail-closed, fail-open-with-alert, or durable-buffer
delivery. The library never silently discards a record and performs no hidden
retry. Repeating the same record ID and canonical bytes is idempotent.

See the [documentation index](docs/README.md), [threat model](docs/threat-model.md),
[delivery semantics](docs/delivery.md), [privacy policy boundary](docs/privacy.md),
and [PostgreSQL operations](docs/postgresql.md).

## License

MIT. See [LICENSE](LICENSE).
