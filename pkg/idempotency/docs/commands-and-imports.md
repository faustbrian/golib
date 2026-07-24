# Commands and imports

`idempotencycommand.Runner` executes a named operation once per stable source
identity and replays its bounded result and metadata.

```go
runner, err := idempotencycommand.New(idempotencycommand.Options{
	Service: service,
	Lease: 2 * time.Minute,
	TransitionTimeout: 5 * time.Second,
})
if err != nil {
	return err
}

fingerprint, err := canonical.JSONFingerprint(
	"widget-row-v1",
	row.Payload,
	canonical.Limits{
		MaxInputBytes: 64 * 1024,
		MaxOutputBytes: 64 * 1024,
		MaxDepth: 32,
	},
)
if err != nil {
	return err
}

result, err := runner.Run(ctx, idempotencycommand.Request{
	Namespace: "imports",
	Tenant: tenantID,
	Name: "widgets.import",
	Caller: "nightly-widget-import",
	SourceID: row.SourcePrimaryKey,
	Fingerprint: fingerprint,
}, func(ctx context.Context) ([]byte, map[string]string, error) {
	ownership, _ := idempotency.OwnershipFromContext(ctx)
	widgetID, err := upsertWidgetWithFence(ctx, row, ownership.FencingToken)
	if err != nil {
		return nil, nil, err
	}
	return []byte(widgetID), map[string]string{"action": "upserted"}, nil
})
```

For a one-off command, use a stable invocation identity such as an approved
change ID, source snapshot ID, or operator-supplied idempotency key. For an
import, call `Run` separately for each source record and use the source system's
immutable primary key. Do not use row position, current time, retry count, or a
random value.

The fingerprint must cover every input that changes the business effect. If a
source ID is reused with changed input, `ErrConflict` is returned rather than
silently overwriting the original meaning.

## Retry behavior

- completed operations replay `Result` and `Metadata` with `Replayed` set;
- handler errors release ownership and can be retried;
- `ErrInProgress` means another unexpired owner is working;
- `ErrConflict` requires source-data or identity investigation;
- `ErrTerminalFailure` replays a deliberately persisted permanent failure;
- storage errors fail closed because command execution status is unknown.

If release fails after the handler error, both errors are joined. Preserve the
full error chain in logs and do not assume the source record is free for an
immediate retry.
