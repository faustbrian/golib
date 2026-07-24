# webhook

`webhook` is a protocol-independent Go module for exact-byte webhook
verification, replay protection, deterministic outbound signing, and bounded
delivery. It uses `net/http`, HMAC-SHA-256 or HMAC-SHA-512, explicit clocks and
limits, and no production `unsafe` or cgo.

The module does not claim support for any vendor preset. The generic `v1`
scheme is specified by [the signature reference](docs/signatures.md) and has
independently generated Python fixtures in `testdata/vectors/v1.json`.

## Install

```sh
go get github.com/faustbrian/golib/pkg/webhook
```

Go 1.26 or newer is required because the optional published `outbox`
adapter requires it.

## Receive

```go
verifier, err := webhook.NewVerifier(webhook.VerifierConfig{
    Algorithm: webhook.SHA256,
    Keys: []webhook.VerificationKey{{ID: "2026-07", Secret: secret}},
    Tolerance: 5 * time.Minute,
})
if err != nil { return err }

handler, err := verifier.Middleware(webhook.MiddlewareConfig{
    Request: webhook.RequestOptions{
        MaxBodyBytes: 1 << 20,
        HeaderLimits: webhook.HeaderLimits{MaxSignatures: 2, MaxBytes: 1024},
    },
}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        body, _ := webhook.VerifiedBodyFromContext(r.Context())
        _ = body // decode only after verification
        w.WriteHeader(http.StatusNoContent)
}))
if err != nil { return err }
```

When replay protection is configured, provide an atomic `ReplayStore` and an
event-ID extractor. See [the inbound guide](docs/inbound.md).

## Send

Construct a `Signer`, a strict `SSRFPolicy`, and a `Deliverer`. A delivery must
have an endpoint and event ID. More than one attempt additionally requires an
idempotency key. Redirects are never followed by `NewSecureHTTPClient`.

See [the outbound guide](docs/delivery.md) and executable examples in
[`example_test.go`](example_test.go).

Optional packages integrate `idempotency`, `log`, `telemetry`,
`queue`, and `outbox`. `webhooktest` supplies deterministic consumer
fixtures.

## Guarantees

- exact received bytes are hashed before application decoding;
- every signing operation uses a signed, injectable random nonce;
- signature comparison uses `hmac.Equal`;
- malformed or duplicate signature fields are rejected deterministically;
- replay storage is atomic and fails closed;
- request, response, header, attempt, DNS, and fan-out work are bounded;
- endpoint policy is checked before every attempt and again at dial time;
- observations exclude payloads, signatures, event IDs, keys, and URLs.

## Documentation

- [API and compatibility](docs/api.md)
- [Inbound verification and raw bodies](docs/inbound.md)
- [Signatures, canonicalization, timestamps, and rotation](docs/signatures.md)
- [Replay and idempotency](docs/replay.md)
- [Delivery, retries, dead letters, and replay](docs/delivery.md)
- [Threat model and SSRF policy](docs/security.md)
- [Adapters and observability](docs/integrations.md)
- [Providers and interoperability](docs/providers.md)
- [Audit matrices and evidence](docs/audit-matrices.md)
- [Operations and troubleshooting](docs/operations.md)
- [Release verdict](docs/release-verdict.md)
- [Migration and SemVer](docs/migration.md)
- [FAQ](docs/faq.md)

## Development

```sh
make check
make safety
make interoperability
```

Security reports follow [SECURITY.md](SECURITY.md). Contributions follow
[CONTRIBUTING.md](CONTRIBUTING.md). The project is MIT licensed.
Dependency attribution is recorded in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
