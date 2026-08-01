# API

`New(Client, kmsKeyID)` validates a least-privilege AWS client. An empty KMS
identifier uses the AWS Secrets Manager service key. A nonempty identifier is
sent only when creating the secret container.

`Store.PutVersion` accepts:

- `Name`: a 1–512 byte AWS-compatible, non-secret name;
- `VersionID`: a 32–64 byte alphanumeric or hyphenated stable token;
- `Stage`: a 1–256 byte AWS-compatible label unique to that version;
  `AWSCURRENT`, `AWSPREVIOUS`, and `AWSPENDING` are rejected; and
- `Value`: 1–65,536 bytes of binary secret material.

For a new name, the adapter calls `CreateSecret`. If AWS reports
`ResourceExistsException`, it calls `PutSecretValue` with the exact
`VersionID`, value, and unique stage. A success is accepted only when AWS
returns a nonempty ARN and the exact requested version identifier.

If `PutSecretValue` reports `ResourceExistsException`, the adapter reads the
exact requested version. It returns the existing reference only when the ARN,
version identifier, binary representation, and constant-time material
comparison all agree. Different existing material returns
`ErrVersionConflict`.

`ErrClientRequired`, `ErrInvalidKMSKey`, `ErrInvalidRequest`,
`ErrOperation`, `ErrInvalidResponse`, and `ErrVersionConflict` support
`errors.Is`. Operational failures retain their original cause through
`errors.Is` and `errors.As` without rendering the cause in the formatted error.
