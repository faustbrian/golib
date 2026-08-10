# http-signature

`http-signature` implements HTTP Message Signatures (RFC 9421) and HTTP digest
fields (RFC 9530) for Go `net/http` messages. The core separates Structured
Fields syntax, signature-base construction, cryptographic algorithms, signing
and verification policy, key resolution, replay consumption, and HTTP
adapters.

It does not replace TLS, authentication, authorization, or capabilities. A
successful verification proves only cryptographic validity and conformance to
the selected application profile.

## Install

```sh
go get github.com/faustbrian/golib/pkg/http-signature@v1
```

## Minimal request signing

```go
key, _ := httpsignature.NewHMACKey(secret)
profile, _ := httpsignature.NewSigningProfile(httpsignature.SigningProfileConfig{
    AllowedAlgorithms: []httpsignature.Algorithm{httpsignature.HMACSHA256},
    CoveredComponents: []httpsignature.ComponentIdentifier{
        {Name: "@method"},
        {Name: "@authority"},
        {Name: "content-digest"},
    },
    Expires: httpsignature.ParameterRequired,
    AlgorithmParameter: httpsignature.ParameterRequired,
    Nonce: httpsignature.ParameterForbidden,
    Tag: httpsignature.ParameterForbidden,
    Lifetime: time.Minute,
    ResolveTimeout: time.Second,
    Now: time.Now,
    Provider: provider,
})

signing, _ := httpsignature.NewSigningRoundTripper(httpsignature.SigningRoundTripperConfig{
    Transport: http.DefaultTransport,
    Signer: httpsignature.NewSigner(profile),
    Label: "sig",
    Existing: httpsignature.ExistingSignaturesReject,
    Options: func(context.Context, *http.Request) (httpsignature.SigningOptions, error) {
        return httpsignature.SigningOptions{}, nil
    },
})

digesting, _ := httpsignature.NewBufferedContentDigestRoundTripper(
    httpsignature.BufferedContentDigestRoundTripperConfig{
        Transport: signing,
        Algorithms: []httpsignature.DigestAlgorithm{httpsignature.SHA256},
        MaxBytes: 1 << 20,
    },
)
client := &http.Client{Transport: digesting}
```

The wrapper order is significant: digest first, signature second, network
transport last. This ensures the signed message already contains the digest.

## Verification model

Create a `VerificationProfile` with explicit allowed algorithms, required
components, time policy, nonce policy, key resolver, resolution timeout, and
optional mandatory external request context. Applications select the signature
label and map typed failures to HTTP responses. Verification metadata stored in
context is not an authorization decision.

Behind a proxy, set `RequireExternalRequestContext` and supply a trusted
`ExternalRequestContext` from deployment configuration. The package never
reads `Forwarded` or `X-Forwarded-*` automatically.

## Bodies and trailers

- `BufferedContentDigestRoundTripper` consumes and closes the outbound caller
  body, delegates a cloned replayable body, and enforces `MaxBytes`.
- `BufferedContentDigestVerificationMiddleware` reads and verifies before
  application code sees the body.
- `TrailerSigningRoundTripper` hashes while the transport reads and emits a
  signed `Content-Digest` trailer at EOF. It is one-attempt and deliberately
  clears `GetBody`; retry policy must create a fresh request.
- `BufferedTrailerVerificationMiddleware` waits for EOF before trusting or
  releasing content, because trailers can be absent or dropped.
- `TrailerResponseSigningMiddleware` streams response bytes, declares digest
  and signature trailers before the final header, and reports any failure that
  occurs after bytes were emitted.
- `BufferedTrailerVerifyingRoundTripper` waits for response EOF, verifies the
  authenticated digest trailers, and only then returns a replayable body.
- `ResponseSigningMiddleware` buffers under an explicit limit and can compute
  and cover `Content-Digest`. Streaming, flushing, hijacking, and full duplex
  use the response trailer adapters instead.

Digest bytes are the HTTP content as presented at the adapter boundary. Place
compression before or after digesting according to whether the digest must
cover coded or uncoded bytes; RFC 9530 `Content-Digest` covers actual message
content, including applied content coding. `Repr-Digest` requires the
application to supply the selected representation bytes when they differ from
message content.

## Supported algorithms

All active IANA RFC 9421 algorithms are implemented with strict key binding:
RSA-PSS-SHA-512, RSA-v1.5-SHA-256, HMAC-SHA-256, ECDSA P-256/SHA-256, ECDSA
P-384/SHA-384, and Ed25519. RSA keys require at least 2048 bits; HMAC keys
require at least 256 bits. RFC 9530 computation supports only active SHA-256
and SHA-512. Deprecated digest algorithms remain parseable for negotiation but
are never computed or accepted as required algorithms.

## Legacy and vendor protocols

The [`compatibility`](compatibility) package provides explicitly named
`RoundTripper` and verification-middleware boundaries for Cavage draft
signatures, AWS Signature V4, OAuth 1.0, and named vendor schemes. Applications
must supply the actual protocol implementation; the boundary clones outbound
request metadata, preserves body ownership, sanitizes callback failures, and
never invokes or extends the RFC 9421 parsers. Do not install a compatibility
adapter and an RFC 9421 adapter as interchangeable authentication paths.

## Documentation

- [Integration and middleware ordering](docs/integration.md)
- [Security model and profiles](docs/security.md)
- [Security and hardening review](docs/security-review.md)
- [Benchmarks](docs/benchmarks.md)
- [Independent comparison benchmarks](benchmarks/comparison/README.md)
- [Normative conformance matrix](docs/conformance.md)
- [FAQ and migration](docs/faq.md)
- [Pinned specifications and errata](spec/errata-decisions.md)
- [Independent interoperability inventory](spec/interoperability.md)
- [Changelog](CHANGELOG.md)

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
