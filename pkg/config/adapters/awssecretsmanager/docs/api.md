# API

`New(Client, Options)` returns a `config.Source`. `Client` contains only
`GetSecretValue`; callers normally satisfy it with the AWS SDK Secrets Manager
client created from `awsconfig.LoadDefaultConfig`.

`Options` contains source metadata, the explicit secret identifier, optional
version ID or stage, and a payload limit. Its default stage is `AWSCURRENT`,
its default priority is `config.PriorityDiscoveredProfile`, and its maximum
payload is 65,536 bytes. Every source is marked sensitive.

`ErrClientRequired`, `ErrInvalidOptions`, `ErrOperation`, and
`ErrInvalidResponse` support `errors.Is`. A provider
`ResourceNotFoundException` becomes `config.ErrNotFound`. Other provider
causes remain available through `errors.Is` and `errors.As`, while formatted
errors expose only the stable redacted operation classification.
