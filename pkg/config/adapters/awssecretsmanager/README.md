# AWS Secrets Manager configuration source

This adapter loads one bounded JSON configuration document from AWS Secrets
Manager into a sensitive `golib/config` source. It leaves AWS credential
resolution, retries, IAM, secret creation, and rotation to the caller.

## Quick start

```go
awsConfig, err := awsconfig.LoadDefaultConfig(ctx)
if err != nil {
    return err
}
source, err := awssecretsmanagerconfig.New(
    secretsmanager.NewFromConfig(awsConfig),
    awssecretsmanagerconfig.Options{
        Name:     "runtime-secrets",
        SecretID: "track/production/runtime",
    },
)
```

The source reads `AWSCURRENT` by default. Supply `VersionID`, `VersionStage`,
or both when startup must be pinned to an immutable version. Place the source
below process-environment overrides in an explicit `config.Plan`.

## Guarantees

- exactly one explicit secret identifier is read per load;
- payload work is bounded by the AWS 65,536-byte service limit;
- both AWS string and binary JSON values are supported;
- missing secrets map to `config.ErrNotFound` for optional-source semantics;
- provider error details and all loaded fields are marked sensitive; and
- no process environment, global AWS configuration, cache, or goroutine is
  owned by the adapter.

## Tradeoffs

Each load performs one provider read. Callers should load once during process
startup unless they intentionally own refresh, version transition, and failure
semantics. The adapter accepts JSON objects only and does not flatten dotenv
text or mutate `os.Environ`.

## Documentation

- [API](docs/api.md)
- [Adoption](docs/adoption.md)
- [Architecture](docs/architecture.md)
- [Compatibility](docs/compatibility.md)
- [Security](docs/security.md)
- [FAQ](docs/faq.md)

## Development

```console
make check MODULES=pkg/config/adapters/awssecretsmanager
```

## License

MIT.

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
