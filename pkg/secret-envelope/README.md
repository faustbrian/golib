# secret-envelope

`secret-envelope` encrypts application-owned secret payloads with one-use
AES-256-GCM data keys and delegates key wrapping to an explicit provider. The
provider-neutral keyring adapter wraps those keys with versioned AES-256 keys
delivered through an application's secret-management boundary. The optional
AWS KMS adapter uses `GenerateDataKey` and `Decrypt` and also exposes a
verify-only asymmetric KMS boundary for bounded externally signed raw
statements. Plaintext data keys are best-effort zeroized after each operation.

## Boundary

Use this module for bounded application payloads persisted in databases or
object storage. Deliver static service credentials and keyring material through
the application's approved secret manager. The module does not manage secret
delivery, rotation workflows, authorization, persistence records, cloud
policies, or logging.

Signature verification authenticates an exact message, key, and reviewed
algorithm. It does not decide whether the signer may approve an action, fetch
signed statements, or expose a signing operation.

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

For secret-manager-delivered wrapping keys, construct a versioned keyring:

```go
keyProvider, err := keyring.New(map[string][]byte{
    "metadata-v1": decodedVersionOneKey,
    "metadata-v2": decodedVersionTwoKey,
})
if err != nil {
    return err
}
envelopes, err := secretenvelope.NewService(keyProvider)
```

Applications select the active reference for new writes and retain every older
key until no persisted envelope refers to it. Keyring values must come from the
approved secret-delivery boundary and must never be committed or logged.

For externally signed statements, construct a verify-only boundary:

```go
verifier, err := awskms.NewSignatureVerifier(
    kms.NewFromConfig(awsConfig),
    types.SigningAlgorithmSpecEcdsaSha256,
)
if err != nil {
    return err
}
if err := verifier.Verify(
    ctx,
    approvalKeyARN,
    canonicalStatement,
    signature,
); err != nil {
    return err
}
```

## Guarantees

- AES-256-GCM with fresh 96-bit nonces from `crypto/rand`.
- A fresh KMS data key for every encrypted payload.
- Stable, bounded, versioned binary persistence format.
- Immutable contexts and envelopes with caller-owned byte copies.
- Redacted text, JSON, and `slog` representations.
- Plaintext payloads bounded to 4 MiB, with bounded wrapped keys, contexts, and
  envelopes.
- Redacted errors that retain `errors.Is` cause traversal.
- Verify-only KMS authentication for raw messages up to 4096 bytes with
  explicit PSS, ECDSA, or Ed25519 algorithms.

## Documentation

- [API and persistence](docs/api.md)
- [Architecture](docs/architecture.md)
- [Security](docs/security.md)
- [Versioned keyrings](docs/keyring.md)
- [AWS KMS operations](docs/aws-kms.md)
- [Compatibility](docs/compatibility.md)

## License

MIT.
