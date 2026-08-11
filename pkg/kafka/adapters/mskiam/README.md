# Kafka Amazon MSK IAM adapter

`mskiam` is the independently versioned Amazon MSK IAM authentication adapter
for [`github.com/faustbrian/golib/pkg/kafka`](../..). The root Kafka module
remains AWS-independent.

Use this adapter only with Amazon MSK clusters whose IAM access control is
enabled. It generates the SASL/OAUTHBEARER token required by non-Java clients
through AWS's supported Go signer. It does not implement SigV4, Kafka SASL, or
credential discovery. It performs one bounded invalidation and retrieval when a
refresh-capable credential provider returns credentials too close to expiry.

## Five-minute setup

The zero credential-provider choice loads the AWS SDK v2 default credential
chain once and retains its concurrency-safe refreshing provider:

```go
provider, err := mskiam.New(ctx, mskiam.Config{
    Region:       "eu-north-1",
    TokenTimeout: 5 * time.Second,
})
if err != nil {
    return err
}

producer, err := kafka.NewProducer(kafka.ProducerConfig{
    Brokers: []string{
        "b-1.example.kafka.eu-north-1.amazonaws.com:9098",
        "b-2.example.kafka.eu-north-1.amazonaws.com:9098",
    },
    ClientID:      "orders-producer",
    AllowedTopics: []string{"orders"},
    Security: kafka.ClientSecurity{
        Authentication: kafka.NewOAuthBearerAuthentication(provider),
    },
})
```

Use the `BootstrapBrokerStringSaslIam` value returned by Amazon MSK rather than
constructing broker endpoints. The root Kafka module requires verified TLS for
OAUTHBEARER and rejects a plaintext transport.

## API reference

- `Config` requires `Region`; `TokenTimeout` is optional and
  `CredentialsProvider` selects an explicit caller-owned AWS SDK v2 provider.
- `Config.Validate` checks region, timeout, and typed-nil provider bounds without
  loading credentials.
- `New` validates configuration, loads the default AWS credential chain when
  needed, and returns a concurrency-safe `Provider`.
- `Provider.Token` implements `kafka.OAuthBearerProvider` and creates one fresh,
  owned token for each authentication session.
- `ProviderError` exposes stable categories through `errors.Is` while discarding
  arbitrary provider and signer diagnostics.

See the compiled [package documentation](https://pkg.go.dev/github.com/faustbrian/golib/pkg/kafka/adapters/mskiam)
for the complete exported API.

## Adoption and tradeoffs

Adopt this module when an AWS IAM identity must authenticate directly to an
IAM-enabled MSK cluster through the root Kafka client. Keep generic OAuth token
issuers and non-AWS Kafka clients outside this adapter.

The default chain provides workload-identity rotation with minimal wiring. An
explicit provider gives callers source selection and test control, but also
makes its concurrency, cancellation, caching, and rotation behavior
caller-owned. Token generation is deliberately per authentication session;
there is no adapter token cache or proactive refresh goroutine. Concurrent
near-expiry requests share one adapter-coordinated invalidation and refreshed
credential result, including a redacted failure, so a shared cache is not
refreshed by every caller in the same request cohort.

## Credential selection and rotation

When `CredentialsProvider` is nil, `New` uses
`config.LoadDefaultConfig(ctx, config.WithRegion(...))`. The AWS SDK default
chain can select environment, web-identity, shared-profile, process, ECS
container, EKS pod-identity, or EC2 instance-role credentials according to SDK
precedence. Its credential cache refreshes expiring credentials on demand.

For ECS, prefer a task IAM role. Do not copy access keys into task definitions,
environment files, broker URLs, logs, or application configuration. EKS
workloads should prefer pod identity or a narrowly scoped web-identity role.

Applications needing a deliberately selected source can supply any current
AWS SDK v2 `aws.CredentialsProvider`:

```go
provider, err := mskiam.New(ctx, mskiam.Config{
    Region:              "eu-north-1",
    CredentialsProvider: credentialsProvider,
    TokenTimeout:        5 * time.Second,
})
```

The supplied provider remains caller-owned. It must be concurrency-safe, honor
context cancellation, and rotate credentials. Wrap providers that do not cache
with `aws.NewCredentialsCache` before construction.

## Token and deadline policy

- AWS region is required and must use canonical lowercase region syntax.
- Token generation defaults to 5 seconds and is configurable from 100
  milliseconds through 1 minute.
- The root Kafka client applies its own bounded credential deadline as an outer
  limit.
- Every authentication session asks the AWS signer for a fresh token.
- The effective `kafka.OAuthBearerToken` expiry is the earlier of the signer
  expiry and the AWS credential expiry.
- Credentials with 30 seconds or less remaining validity are invalidated and
  retrieved once more when the provider exposes the AWS SDK cache invalidation
  contract; concurrent callers share that refresh transition, and providers
  without invalidation support fail closed.
- Tokens with 30 seconds or less remaining validity, more than 20 minutes of
  lifetime, malformed URL-safe base64, or more than 1 MiB are rejected.
- Signer timestamps outside a five-minute tolerance of the adapter clock are
  rejected. This detects inconsistent signer output; it cannot prove the host
  clock is synchronized with Amazon MSK.
- Cancellation stops waiting only when the selected AWS credential provider
  and signer honor the supplied context; the supported AWS SDK path does.
- The provider starts no goroutines. It performs no proactive refresh.

The signer currently issues 15-minute tokens. Credential lifetime and token
lifetime are different: an AWS credential provider may refresh the underlying
role credentials before signing, and the adapter never reports a token as valid
beyond the credential used to sign it.

### Token transition model

| State | Success transition | Failure transition |
| --- | --- | --- |
| credential retrieval | validate credential fields and lifetime | redacted retrieval, invalid-credential, cancellation, timeout, or panic category |
| near-expiry credential | coordinate one invalidation and refresh | fail closed when refresh is unsupported, invalid, canceled, or still near expiry |
| signer call | receive token bytes and signer expiry | redacted signer, cancellation, timeout, or panic category |
| validation | bind URL shape, region, timestamp, lifetime, and size | malformed or expired token category |
| return | copy token bytes and cap expiry at credential expiry | no partial token is returned |

Every transition is synchronous and context-bounded. The adapter creates only
the per-call deadline timer, starts no goroutine, and leaves credential-provider
ownership and shutdown with the caller.

## Failure and redaction

Configuration, credential-chain loading, credential retrieval, signer failure,
provider panic, cancellation, timeout, expiry, and malformed signer results
have stable error categories. `ErrMalformedToken` and `ErrTokenExpired` retain
`ErrInvalidToken` compatibility. `ProviderError.Error` and `GoString` never
render the AWS SDK or signer diagnostic. Arbitrary provider and signer causes
are not retained in returned errors; `context.Canceled` and
`context.DeadlineExceeded` identity remain available through `errors.Is`.

The adapter never logs. It does not enable or mutate the signer's process-wide
`AwsDebugCreds` flag, and token generation fails closed before the signer call
when that flag is enabled. Applications must configure the flag before starting
concurrent work and leave it disabled for the process lifetime; changing the
upstream global concurrently is a data race. Access keys, secret keys, session
tokens, signed tokens, credential endpoints, and provider diagnostics must not
be exported to logs, traces, metrics, panic output, or fixtures.

## IAM authorization

MSK IAM performs both authentication and authorization. A valid signed token
does not grant Kafka access. Use least-privilege `kafka-cluster` permissions
scoped to the exact cluster, topic, group, and transactional-ID resources:

- producers require connect, topic description, and write permissions;
  idempotent production with topic-scoped writes additionally requires the
  cluster-level `kafka-cluster:WriteDataIdempotently` action and access to the
  transactional-ID resources required by AWS's IAM implementation;
- consumers additionally require group description/alteration and read
  permissions;
- transactions require transactional-ID description and alteration, and IAM
  transaction termination requires Kafka 3.8 or later because earlier broker
  versions do not expose the internal `WriteTxnMarkers` action through IAM;
  use SCRAM or mTLS with appropriate Kafka ACLs on earlier versions; and
- inspection may require dynamic cluster or topic configuration description.

Do not grant `kafka-cluster:*` merely to make authentication pass. Apache Kafka
ACLs do not authorize IAM identities.

## Compatibility status

This adapter pins:

- `aws-msk-iam-sasl-signer-go` v1.0.4;
- AWS SDK for Go v2 v1.43.0 and config v1.32.31;
- `franz-go` v1.21.5 through the root Kafka module; and
- Go 1.26.5.

AWS documents non-Java IAM clients for MSK Kafka 2.7.1 and newer. That protocol
floor is not an operational support claim. Local tests prove signing, root
provider interoperability, default-chain selection, expiry, cancellation,
environment/profile/ECS/EKS credential-source fixtures, pod-identity token
rotation, workload replacement, refresh contention, panic containment, race
safety, and redaction. No Amazon MSK Provisioned or Serverless cluster has yet
been exercised by repository CI, so both remain **unverified**, not supported.

Primary references:

- [Configure clients for IAM access control](https://docs.aws.amazon.com/msk/latest/developerguide/configure-clients-for-iam-access-control.html)
- [IAM access control](https://docs.aws.amazon.com/msk/latest/developerguide/iam-access-control.html)
- [IAM action and resource semantics](https://docs.aws.amazon.com/msk/latest/developerguide/kafka-actions.html)
- [AWS SDK credential providers](https://docs.aws.amazon.com/sdkref/latest/guide/standardized-credentials.html)
- [AWS MSK IAM SASL Signer for Go](https://github.com/aws/aws-msk-iam-sasl-signer-go)

## FAQ

### Does this adapter authorize Kafka operations?

No. It creates the AWS-supported authentication token. IAM policies remain the
authorization source, and the adapter never evaluates or grants permissions.

### Does it implement SigV4 or SASL/OAUTHBEARER?

No. AWS's signer owns signing and the root Kafka module owns SASL. This adapter
only composes credentials, bounded token generation, validation, and expiry.

### Should applications cache the returned token?

No. Give the provider to `kafka.NewOAuthBearerAuthentication`; the Kafka client
requests a fresh token for each authentication session. The current franz-go
callback consumes the token value for that session; it does not schedule a
proactive reconnect from `ExpiresAt`.

### Can one provider be shared by producers and consumers?

Yes, when the selected credentials provider is concurrency-safe. The retained
AWS SDK default provider satisfies that requirement; explicit providers must
satisfy it themselves.

### Are MSK Provisioned and Serverless verified in CI?

No. The signer and Kafka adapter seams are locally verified, but live
Provisioned and Serverless clusters are both explicitly unverified.

## Verification

```sh
make check
```

The local module contract covers formatting, vet, tests, race detection, exact
statement coverage, fuzz smoke, signer interoperability, allocation-reporting
generation, contention, and external-retrieval benchmarks, and documentation.
Repository gates additionally enforce mutation, API compatibility,
vulnerability, secrets, licenses, SBOM, provenance, and clean-consumer checks.
