# Compatibility

The public contract follows semantic versioning. `ClientRequestToken` is the
stable compatibility boundary for immutable retries and becomes the AWS
Secrets Manager version identifier.

The module targets AWS SDK for Go v2 and does not expose SDK response objects.
Callers can upgrade AWS transport behavior independently while the returned
`Reference` remains limited to ARN and version identifier.

Changes to validation bounds, accepted name characters, staging semantics,
error classification, the write-only `Client` contract, or returned reference
fields are compatibility-sensitive and require explicit release notes and
regression coverage.
