# secret-envelope

`secret-envelope` encrypts application-owned secret payloads with one-use
AES-256-GCM data keys and delegates key wrapping to an explicit provider. The
AWS KMS adapter uses `GenerateDataKey` and `Decrypt`; plaintext data keys are
best-effort zeroized after each local operation.

## Boundary

Use this module for dynamic encrypted database payloads that must remain
transactionally coupled to application state. Use AWS Secrets Manager for
deployment and static service credentials. The module does not manage secret
rotation workflows, authorization, database rows, IAM policies, or logging.

Encryption context is mandatory and authenticated by both AES-GCM and the key
provider. Context values are non-secret because AWS KMS can expose them in
CloudTrail. Bind each payload to stable identifiers such as service, owner,
record, and field.

## Example

```go
awsConfig, err := config.LoadDefaultConfig(ctx)
if err != nil {
    return err
}
kmsProvider, err := awskms.New(kms.NewFromConfig(awsConfig))
if err != nil {
    return err
}
envelopes, err := secretenvelope.NewService(kmsProvider)
if err != nil {
    return err
}
binding, err := secretenvelope.NewContext(map[string]string{
    "service":   "location",
    "source_id": sourceID,
    "field":     "vendor_metadata",
})
if err != nil {
    return err
}
encrypted, err := envelopes.Encrypt(ctx, secretenvelope.EncryptRequest{
    Plaintext:    canonicalMetadata,
    KeyReference: kmsKeyARN,
    Context:      binding,
})
if err != nil {
    return err
}
persisted, err := encrypted.MarshalBinary()
```

Applications must load AWS configuration with the SDK default credential
chain. Static credentials are not required by this module.

## Guarantees

- AES-256-GCM with fresh 96-bit nonces from `crypto/rand`.
- A fresh KMS data key for every encrypted payload.
- Stable, bounded, versioned binary persistence format.
- Immutable contexts and envelopes with caller-owned byte copies.
- Redacted text, JSON, and `slog` representations.
- Bounded plaintext, wrapped-key, context, and envelope sizes.
- Redacted errors that retain `errors.Is` cause traversal.

## Documentation

- [API and persistence](docs/api.md)
- [Architecture](docs/architecture.md)
- [Security](docs/security.md)
- [AWS KMS operations](docs/aws-kms.md)
- [Compatibility](docs/compatibility.md)

## License

MIT.
