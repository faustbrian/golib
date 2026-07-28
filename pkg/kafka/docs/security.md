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

## Executed broker evidence

The pinned Apache Kafka 4.3.1 integration fixtures generate a short-lived CA,
broker identity, client identity, usernames, passwords, signing key, JWKS, and
JWTs for each isolated run without writing generated material to the host
filesystem. Only broker-required material is copied into the ephemeral
container; the OAuth private signing key remains in the test process. Bootstrap
assigns credential files mode `0600` to the image's UID 1000, and both Kafka
storage formatting and the broker run as that unprivileged identity. Startup
diagnostics are bounded and redact every generated broker password before test
output.

The fixtures prove:

- exact TLS 1.2 and TLS 1.3 negotiation with normal root and hostname
  verification, plus rejection of an unknown root and wrong hostname;
- mTLS producer delivery, consumer settlement, and inspector health through a
  bounded `ClientCertificateProvider`, plus rejection when the client
  certificate is absent;
- PLAIN, SCRAM-SHA-256, and SCRAM-SHA-512 only over `SASL_SSL`, with successful
  provider-backed production, SCRAM consumption, inspection, and rejection of
  incorrect credentials;
- OAUTHBEARER over `SASL_SSL` using RS256-signed JWTs, a broker-loaded JWKS,
  exact issuer and audience validation, provider-backed production and
  consumption, rejection of signed tokens for the wrong issuer or audience,
  and no token disclosure through the returned authentication error; and
- KRaft `StandardAuthorizer` denial for an authenticated PLAIN principal with
  no matching ACL, including `ErrorAuthorization` producer classification and
  unchanged inspector `TopicAuthorizationFailed` identity without password
  disclosure.

This proves interoperability only with the pinned Apache fixture. Repeated
same-client credential expiry, JWKS refresh and signing-key rollover, TLS
certificate rotation during live traffic, consumer-group and transactional-ID
authorization failures, ACL changes during live traffic, and managed-service
authentication remain separate required evidence. The fixture
does not use Kafka's non-production unsecured OAUTHBEARER implementation and
does not claim compatibility with a particular OAuth identity provider.

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
