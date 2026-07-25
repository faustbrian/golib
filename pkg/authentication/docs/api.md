# Exported API map

Every exported identifier has Go documentation and compiled examples are
checked in CI. Use `go doc` for signatures and contracts.

- Root: `Principal`, `PrincipalSpec`, credentials, results, `Failure`,
  `Challenge`, `Authenticator`, `Composite`, context helpers, and
  instrumentation.
- `basic`: immutable static Basic entries and authenticator.
- `bearer`: opaque-token validator interface, function adapter, and bounds.
- `apikey`: callback validator, atomically replaceable static entries, and
  bounds.
- `authhttp`: explicit sources, extractor, challenge formatting, and middleware
  policies.
- `authlog`: standard `slog` instrumenter.
- `authtest`: deterministic principals, results, clock, scripted authenticator,
  HTTP fixture, and assertions.
- `jwt`: strict config, validator, provider adapters, and owned remote JWK
  lifecycle.
- `oidc`: discovery/static-key config, validator, and nonce interface.
- `authotel`: OpenTelemetry provider config and instrumenter.

Challenge formatting is bounded by `MaxChallengeParameters`,
`MaxChallengeSchemeBytes`, `MaxChallengeNameBytes`, and
`MaxChallengeValueBytes`. OIDC remote policy is configured with
`MinRefreshInterval`, `MaxRefreshInterval`, and `MaxRefreshWaiters`; zero
values select safe defaults.

`BearerQuery` and `APIKeyQuery` remain available for compatibility but are
deprecated for new designs because credential-bearing URLs can be retained
outside this package.

The examples are discoverable with:

```sh
go test -run '^Example' ./...
(cd jwt && go test -run '^Example' ./...)
(cd oidc && go test -run '^Example' ./...)
(cd authotel && go test -run '^Example' ./...)
```
