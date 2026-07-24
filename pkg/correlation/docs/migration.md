# Migration from Cline Correlation

Inventory the old middleware, header names, context accessors, logger fields,
queue metadata, and any business decisions made from correlation values.
Remove business decisions first; this package intentionally has no equivalent
authorization, tenant, or idempotency API.

Map the old workflow identifier to `CorrelationID`, create a new `RequestID`
at each boundary, and set `CausationID` from the immediate prior request. Do
not preserve a legacy request ID across retries. Install explicit adapters in
parallel, compare propagation in redacted logs, then remove the old global or
ambient accessor. Header aliases should be temporary codec configuration with
a documented retirement date.
