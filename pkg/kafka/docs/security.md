# Security

Verified TLS is the zero-value transport policy. `ClientSecurity{}` uses system
roots, enforces TLS 1.2 or newer, and performs normal certificate and hostname
verification. Caller-provided `tls.Config` values are cloned and validation
rejects `InsecureSkipVerify`, obsolete minimum versions, and inconsistent
version bounds.

Custom TLS configuration is fail-closed. Key logging, renegotiation, raw
`GetClientCertificate` callbacks, unreviewed ECH configuration, insecure or
TLS-1.3-only entries in the TLS-1.2 cipher-suite field, unknown curves,
duplicates, unsupported TLS versions, and server-only TLS fields are rejected.
Static client certificates must contain parseable bounded chains and a matching
signing key. Use `ClientCertificateProvider` instead of a raw TLS callback for
rotation.

Unencrypted transport is available only through
`DevelopmentPlaintextSecurity()`. It is intended for isolated local fixtures;
validation forbids TLS material, credentials, and authentication in that mode.

## Authentication and rotation

The root module owns provider contracts for:

- SASL/PLAIN;
- SCRAM-SHA-256 and SCRAM-SHA-512;
- OAUTHBEARER tokens with a required future expiry; and
- mTLS client certificates.

Authentication constructors retain the supplied provider. Providers may be
called concurrently, must honor their context, and must return before
`CredentialTimeout`, which defaults to five seconds and is bounded from 100
milliseconds to one minute. Cancellation bounds the package's request; Go
cannot forcibly stop a provider that ignores its context. Provider panics and
failures become classifiable, redacted errors. Username, password, token,
authorization ID, extension, certificate, and request metadata are validated
and bounded before use. Returned byte slices and maps are copied.

Username, password, and authorization identifiers are valid UTF-8, exclude NUL,
and are limited to 8 KiB each. OAUTHBEARER tokens follow RFC 6750 `b64token`
syntax and are limited to 1 MiB. OAuth extensions follow RFC 7628 framing: at
most 32 alphabetic keys of at most 128 bytes, values of at most 8 KiB using the
permitted ASCII value characters, and no reserved `auth` key. OAuth
authorization IDs reject unescaped GS2 separators and control characters.

Use a static certificate in `tls.Config.Certificates` or a rotating
`ClientCertificateProvider`, but not both. The package clones TLS slice and
certificate bytes and caller root pools. Private-key implementations, TLS
callbacks, client-session caches, and provider interface values cannot be
deep-copied; callers own them for the client lifetime and must keep them
immutable or concurrency-safe.

A TLS configuration may contain at most 16 client identities, 16 ALPN values,
64 safe TLS-1.2 cipher suites, and 32 known curve preferences. Each ALPN value
is 1 to 255 bytes. Each certificate chain contains at most 16 certificates;
the chain, OCSP staple, and signed-certificate timestamps together are limited
to 1 MiB. A broker client-certificate request exposed to a provider is limited
to 64 acceptable CAs totaling 64 KiB and 64 signature schemes before copying.

`String` and `GoString` output for security policies, credentials, and tokens is
redacted. Provider errors preserve `errors.Is` identity through wrapping but do
not render the provider's possibly sensitive message. Applications must apply
the same rule to errors they unwrap and to their own provider diagnostics.

MSK IAM authentication and OpenTelemetry remain absent from the root module.
The independently versioned [`adapters/mskiam`](../adapters/mskiam) module uses
AWS's supported Go signer and either the refreshing SDK v2 default credential
chain or one explicit caller-owned provider. It bounds token generation,
invalidates and retrieves nearly expired cached credentials once, caps
effective token expiry at the signing credential expiry, rejects nearly expired
or malformed tokens, contains panics, and redacts provider diagnostics. It
does not retain arbitrary provider causes in returned errors and never enables
the signer's global credential-debug flag. No MSK compatibility claim is made
without direct Provisioned or Serverless evidence.

The authentication wire rules follow [RFC 4616](https://www.rfc-editor.org/rfc/rfc4616)
for PLAIN, [RFC 5802](https://www.rfc-editor.org/rfc/rfc5802) for SCRAM,
[RFC 7628](https://www.rfc-editor.org/rfc/rfc7628) for OAUTHBEARER, and
[RFC 6750](https://www.rfc-editor.org/rfc/rfc6750) for bearer-token syntax.

Credentials remain caller-owned and must come from a runtime secret provider.
They must not be embedded in topic names, client IDs, transactional IDs, logs,
metrics, traces, errors, fixtures, or replay audit records.

Use least-privilege ACLs per service role. Producers need only approved topic
write access, consumers need their topic read and group access, replay roles
must be separately authorized, and inspectors need read-only metadata and group
describe access. This module intentionally exposes no topic mutation, ACL
mutation, or consumer-group offset mutation.
