# http-middleware

[![CI](https://github.com/faustbrian/golib/pkg/http-middleware/actions/workflows/ci.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/http-middleware/actions/workflows/ci.yml)
[![Release](https://github.com/faustbrian/golib/pkg/http-middleware/actions/workflows/release.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/http-middleware/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/faustbrian/golib/pkg/http-middleware.svg)](https://pkg.go.dev/github.com/faustbrian/golib/pkg/http-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/faustbrian/golib/pkg/http-middleware)](https://goreportcard.com/report/github.com/faustbrian/golib/pkg/http-middleware)

Explicit, bounded server-side HTTP middleware built on `net/http`. The root
package composes only the middleware supplied by the caller. There is no
registry, router, server runtime, service container, background process, or
global default chain.

## Five-minute chain

```go
must := func(value middleware.Middleware, err error) middleware.Middleware {
    if err != nil {
        panic(err) // Fail startup on invalid transport policy.
    }
    return value
}

ids := must(requestid.New(requestid.Policy{}))
recoverPanics := must(recovery.New(recovery.Policy{}))
limitBodies := must(bodylimit.New(bodylimit.Policy{MaxBytes: 1 << 20}))
security := must(secureheader.New(secureheader.APIDefaults()))

chain, err := middleware.New(
    recoverPanics,
    ids,
    limitBodies,
    security,
)
if err != nil {
    panic(err)
}
handler, err := chain.Handler(application)
if err != nil {
    panic(err)
}
if err := http.ListenAndServe(":8080", handler); err != nil {
    panic(err)
}
```

The first middleware receives the request first. The response returns through
the list in reverse. `Handler` rejects nil terminals, invalid descriptors, and
middleware that returns nil.

## Independently importable middleware

Every subpackage returns the standard `func(http.Handler) http.Handler` shape:

```go
limit, err := bodylimit.New(bodylimit.Policy{MaxBytes: 8 << 20})
if err != nil {
    return err
}
handler := limit(uploadHandler)
```

Importing the root does not import codecs, telemetry SDKs, authentication,
authorization, rate limiting, idempotency, routing, or server lifecycle.

## Packages

| Package | Owns | Important boundary |
|---|---|---|
| root | chains, descriptors, conditions | 256-layer maximum |
| `requestid` | request/correlation IDs | inbound values untrusted by default |
| `recovery` | panic containment | never rewrites committed responses |
| `bodylimit` | encoded request byte limit | no payload decoding |
| `deadline` | context and buffered handler timeouts | no arbitrary-code interruption |
| `proxy` | effective peer/host/scheme/prefix | explicit trusted networks only |
| `cors` | server CORS responses and preflights | not authentication or CSRF |
| `secureheader` | explicit response headers | HSTS requires acknowledgement |
| `compress` | bounded gzip negotiation | buffered mode is not streaming |
| `observe` | bounded completion events | no raw path, query, body, or headers |
| `content` | 406/415 media guards | no encoder or decoder |
| `admission` | local in-flight bounds | not distributed rate limiting |
| `responsepolicy` | no-store and readiness admission | no response cache |
| `adapter` | ownership names and overlap checks | no sibling policy reimplementation |
| `middlewaretest` | deterministic fixtures | test-only behavior helpers |

## Recommended API order

`recovery -> trusted proxy -> request ID -> observe -> CORS -> security
headers -> admission -> deadline/body limit -> owning-package policies ->
compression -> application`

Order is application-specific. Compression must remain outside handlers whose
already-encoded responses it should observe, and recovery must surround every
layer whose panic it should contain. See [the ordering reference](docs/ordering.md).

## Security posture

Defaults deny forwarding and inbound identifier trust. All configured header
values reject controls. Parsing and retained buffers have explicit limits.
These controls complement, but do not replace, ingress validation, server
timeouts, TLS, request-header limits, authentication, authorization, CSRF
defenses, or application validation.

Read [SECURITY.md](SECURITY.md), [the threat model](docs/threat-model.md), and
[deployment guidance](docs/security.md) before production use.

## Compatibility

The minimum toolchain is Go 1.26.5. Normal tracking and header wrappers preserve
the exact `Flusher`, `Hijacker`, `Pusher`, and `ReaderFrom` set of the underlying
writer. Buffered timeout and compression intentionally do not expose streaming
interfaces. See [the complete matrix](docs/responsewriter.md).

## Development

```sh
make check
```

Every blocking CI command has a local target. `make nilaway` is advisory.
Hosted release publication remains separate from local verification.

The CI badge covers the blocking `quality`, `lint`, `staticcheck`, and
`vulnerability` jobs. Release verification repeats `make check` before a
signed provenance attestation and release assets are published.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
