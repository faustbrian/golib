# Log signal stability

This module deliberately exposes no stable OpenTelemetry log provider,
exporter, bridge, or global registration.

Trace and metric APIs use stable OpenTelemetry Go packages. The log API and SDK
must reach a compatibility level that can follow the same Go and OTel matrix,
shutdown aggregation, failure injection, privacy review, and coverage gates
before becoming part of the root lifecycle.

Until then, applications may use their normal structured logger and include
trace and span IDs through application-owned code. Do not treat log records as
safe merely because they are correlated: apply independent redaction, size,
retention, and access policies.
