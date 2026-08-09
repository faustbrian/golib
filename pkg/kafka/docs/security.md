# Security

Verified TLS is the zero-value transport policy. `ClientSecurity{}` uses system
roots, enforces TLS 1.2 or newer, and performs normal certificate and hostname
verification. Caller-provided `tls.Config` values are cloned and validation
rejects `InsecureSkipVerify`, obsolete minimum versions, and inconsistent
version bounds.

Custom TLS configuration is fail-closed. Key logging, renegotiation, raw
`GetClientCertificate` callbacks, unreviewed ECH configuration, insecure or
TLS-1.3-only entries in the TLS-1.2 cipher-suite field, unknown curves,
duplicates, unsupported TLS versions, invalid configured server names, and
server-only TLS fields are rejected. A rotating trust provider also rejects an
invalid broker-advertised server name before invoking the provider.
Static client certificates must contain parseable bounded chains and a matching
signing key. Use `ClientCertificateProvider` instead of a raw TLS callback for
client-identity rotation. Use `TrustAnchorProvider` instead of mutating
`tls.Config.RootCAs` for server trust rotation.

Unencrypted transport is available only through
`DevelopmentPlaintextSecurity()`. It is intended for isolated local fixtures;
validation forbids TLS material, credentials, and authentication in that mode.

## Authentication and rotation

The root module owns provider contracts for:

- SASL/PLAIN;
- SCRAM-SHA-256 and SCRAM-SHA-512;
- OAUTHBEARER tokens with a required future expiry;
- mTLS client certificates; and
- server trust anchors for each new TLS connection.

Authentication constructors and `ClientSecurity` retain supplied providers.
Providers may be called concurrently, must honor their context, and must return
before `CredentialTimeout`, which defaults to five seconds and is bounded from
100 milliseconds to one minute. Cancellation bounds the package's request; Go
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
certificate bytes and caller root pools. Use static `tls.Config.RootCAs` or a
rotating `TrustAnchorProvider`, but not both. A trust provider returns the
complete root set for one new connection as 1 to 64 distinct DER-encoded X.509
certificates totaling at most 1 MiB. Ownership of the returned slices and bytes
transfers to the package, so providers must not mutate or reuse them after
returning; the package retains only parsed owned copies. Client session caches
are rejected with a rotating trust provider so a resumed session cannot retain
certificate state from the previous root set.
Private-key implementations, TLS callbacks, client-session caches, and provider
interface values cannot be deep-copied; callers own them for the client
lifetime and must keep them immutable or concurrency-safe.

Trust rotation is overlap-first: publish both old and new roots, rotate the
broker certificate, ensure old connections reconnect within a reviewed bound,
then publish only the new roots. Changing a provider result does not alter an
existing TLS connection. Provider failure, panic, invalid or duplicate roots,
and missing roots fail the new connection with a classifiable redacted error;
the package never disables hostname or chain verification.

A TLS configuration may contain at most 16 client identities, 16 ALPN values,
every distinct safe TLS-1.2 cipher suite exposed by the selected Go toolchain,
and all seven explicitly supported curve preferences. Each ALPN value is 1 to
255 bytes. Each certificate chain contains at most 16 certificates;
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
  disclosure; input-ordered consumer-group inspection additionally retains one
  explicitly authorized group result beside a separate
  `GroupAuthorizationFailed` result with stable `ErrorAuthorization`
  classification and no password disclosure; and
- live PLAIN credential replacement through broker restart: three independent
  producers use the replacement password from their providers after the
  secured broker returns, preserve every acknowledged record, and reject the
  retired password on a new connection; and
- live SCRAM-SHA-256 and SCRAM-SHA-512 credential replacement: three
  independent producers per mechanism cross Kafka's three-second
  broker-enforced reauthentication lifetime through three successive
  replacements, invoke every package provider again, preserve every
  acknowledged record, and reject every retired credential on a new
  connection; and
- live mTLS client-certificate renewal: three independent producers observe
  Kafka's broker-enforced idle disconnect, invoke every package provider with a
  separately issued replacement certificate signed by the same CA, reconnect,
  and preserve every acknowledged record in exact broker order; and
- live server-certificate and trust-anchor rotation: Apache Kafka dynamically
  replaces the client-listener keystore with a certificate under a separately
  generated CA, an existing producer reconnects using overlapping roots, then
  reconnects again after the retired root is removed; a new client trusting
  only the retired root is rejected.

Kafka's default PLAIN login module keeps the accepted passwords in broker JAAS
configuration. Kafka 4.3.1's existing SASL channel builder dynamically reloads
TLS material, not its JAAS context. A same-principal password change therefore
requires a broker or listener restart and creates a mixed-credential window in
a rolling cluster. Use an external server callback handler or introduce a
second principal with overlapping least-privilege ACLs before removing the old
principal when zero-downtime rotation is required. The package credential
provider can supply either client credential, but it cannot make the broker
verifier transition atomic. See Kafka's
[PLAIN production guidance](https://kafka.apache.org/43/security/authentication-using-sasl/#use-of-sasl-plain-in-production)
and the pinned
[SASL channel reconfiguration source](https://github.com/apache/kafka/blob/4.3.1/clients/src/main/java/org/apache/kafka/common/network/SaslChannelBuilder.java#L189-L207).

This proves interoperability only with the pinned Apache fixture. Prolonged
multi-client mTLS rollover stress, OAuth rotation stress, zero-downtime
multi-broker PLAIN cutover, JWKS refresh and signing-key rollover,
transactional-ID authorization failures, ACL changes during live traffic, and
managed-service authentication remain separate required evidence. The fixture
does not use Kafka's
non-production unsecured OAUTHBEARER implementation and does not claim
compatibility with a particular OAuth identity provider.

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
