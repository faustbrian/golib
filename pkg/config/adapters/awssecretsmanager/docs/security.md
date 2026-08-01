# Security

Use workload identity and the AWS SDK default credential chain. Do not provide
static AWS access keys to this adapter. IAM should allow only
`secretsmanager:GetSecretValue` for the configured secret and the KMS decrypt
permission required by that secret.

The source identifier and version selectors are configuration metadata, not
secret material. Payload values are marked sensitive and provider error
details are redacted from formatting. The transient adapter-owned payload copy
is cleared after parsing; the returned configuration document necessarily owns
its decoded values for the lifetime chosen by the caller.

Avoid logging source documents, typed settings, AWS responses, or wrapped
provider causes. Rotation and refresh must be an explicit application workflow
with validated transition and rollback behavior.
