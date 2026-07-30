# AWS Secrets Manager immutable version store

This module creates or idempotently confirms immutable, explicitly versioned
binary secrets through AWS Secrets Manager. It is intended for migrations and
other workflows that must persist a stable external secret reference without
storing plaintext in an application database.

## Quick start

Load AWS configuration through the SDK default credential chain, construct a
Secrets Manager client, and pass it to `New`. `PutVersion` accepts a stable
secret name, a 32–64 byte `ClientRequestToken`, a version-unique staging label,
and a bounded binary value. It returns only the secret ARN and exact version
identifier.

```go
awsConfig, err := config.LoadDefaultConfig(ctx)
if err != nil {
    return err
}
store, err := awssecretstore.New(
    secretsmanager.NewFromConfig(awsConfig),
    "alias/application-secrets",
)
if err != nil {
    return err
}
reference, err := store.PutVersion(ctx, awssecretstore.PutVersionRequest{
    Name:      secretName,
    VersionID: versionID,
    Stage:     "migration-" + versionID,
    Value:     plaintext,
})
```

The caller owns AWS configuration, authorization, stable name and version
derivation, plaintext lifecycle, and persistence of the returned reference.

## Guarantees

- A new name creates one Secrets Manager secret and its initial immutable
  version.
- An existing name receives `PutSecretValue` with the exact caller-supplied
  `ClientRequestToken`.
- Exact retries are idempotent under AWS Secrets Manager semantics.
- Reusing a version token with different material fails instead of mutating the
  existing version.
- Historical writes use a unique staging label and never move a shared label
  such as `AWSCURRENT`.
- AWS-managed `AWSCURRENT`, `AWSPREVIOUS`, and `AWSPENDING` labels are rejected
  at validation.
- Inputs are bounded and copied before AWS calls; the internal payload copy is
  best-effort zeroized afterward.
- Errors never format secret values or request fields.

## Tradeoffs

The adapter performs create-then-put-on-existence because AWS has distinct APIs
for creating a secret container and adding a version. It does not compare or
order application versions, rotate secrets, update staging labels, read secret
values, delete versions, or manage IAM and KMS policy.

## Documentation

- [API](docs/api.md)
- [Architecture](docs/architecture.md)
- [Security](docs/security.md)
- [Compatibility](docs/compatibility.md)
- [Adoption](docs/adoption.md)
- [FAQ](docs/faq.md)

## License

MIT.
