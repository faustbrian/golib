# Security

Use workload identity and the AWS SDK default credential chain. Do not provide
static AWS credentials through this module.

Grant only `secretsmanager:CreateSecret` and
`secretsmanager:PutSecretValue` and version-pinned
`secretsmanager:GetSecretValue` for the approved prefix, plus the minimum KMS
permissions when a customer-managed key is configured. Enforce the prefix and
key in IAM because caller-provided names remain an authorization input.

Names, version identifiers, stages, KMS identifiers, and AWS request metadata
are non-secret and may appear in CloudTrail. Values must be passed only through
`SecretBinary`. Errors and documentation must never include those values.

The adapter copies the value before the SDK call and best-effort zeroizes that
copy afterward. Go cannot guarantee memory erasure, and the caller remains
responsible for its original slice and any upstream decrypted representation.
An existing-version retry temporarily reads that exact binary version solely
for constant-time equality verification; it is not returned to the caller.

Use a unique stage for every version. Reusing `AWSCURRENT`, `AWSPREVIOUS`, or
another shared stage could move that label and invalidate historical ordering.
