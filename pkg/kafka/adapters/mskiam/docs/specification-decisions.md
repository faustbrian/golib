# Kafka Amazon MSK IAM adapter specification decisions

This register records observable choices at the AWS signer, credential,
SASL/OAUTHBEARER, and managed-service compatibility boundaries. Exact source
archives and reviewed documentation snapshots are pinned in
[`specification/manifest.json`](../specification/manifest.json).

## KAFKA-MSKIAM-DEC-001: AWS owns signing while the adapter validates output

- **Status, owner, and classification:** `resolved`; Kafka MSK IAM adapter maintainers; signer and token trust boundary.
- **Source and issue:** The [AWS MSK IAM signer for Go](https://github.com/aws/aws-msk-iam-sasl-signer-go) owns SigV4 token generation, but accepting arbitrary signer output would permit malformed, oversized, wrong-region, or wrongly scoped bearer tokens into Kafka authentication.
- **Credible interpretations and known peer behavior:** The adapter could reimplement SigV4, trust every successful signer result, expose raw signer errors, or delegate signing while validating the bounded presigned-URL contract. Non-Java clients commonly delegate to AWS's supported signer.
- **Selected behavior:** The adapter never implements SigV4. It requests a fresh AWS signer token, validates canonical scheme, host, action, region, expiry, signed fields, and encoded size, then returns an owned token with redacted failure categories.
- **Security, resource, compatibility, and wire consequences:** Security retains AWS's signing implementation while rejecting confused or hostile output; resource work is bounded by token and field limits; compatibility is tied to signer v1.0.4; wire output is the validated SASL/OAUTHBEARER token consumed by the root Kafka client.
- **Executable evidence and public surface:** `TestProviderGeneratesOwnedExpiringMSKIAMToken`, `TestTokenRejectsInvalidSignerResults`, and `TestSignedTokenValidationRejectsEachMalformedField` cover `Provider`, `Token`, and the signer boundary.
- **Upstream record and reconsideration:** The upstream record is signer v1.0.4 and AWS MSK IAM client guidance. Reconsider on signer format, IAM action, hostname, or SASL integration changes.

## KAFKA-MSKIAM-DEC-002: Credentials refresh per bounded authentication cohort

- **Status, owner, and classification:** `resolved`; Kafka MSK IAM adapter maintainers; credential lifecycle and concurrency.
- **Source and issue:** The [AWS SDK credential-provider contract](https://docs.aws.amazon.com/sdkref/latest/guide/standardized-credentials.html) supports rotating sources, while token generation and credential expiry can overlap under concurrency. Static process credentials or caller cancellation shared across a cohort can produce stale tokens or cross-request failure.
- **Credible interpretations and known peer behavior:** The adapter could cache forever, bypass SDK caching, refresh independently per goroutine, or share one bounded refresh while preserving each caller's cancellation. AWS SDK consumers commonly rely on provider caches but differ in outer token caching.
- **Selected behavior:** Every authentication session obtains current credentials through the caller-selected or default SDK provider. Near-expiry refresh is shared once, token expiry is capped by credential expiry, caller cancellation does not cancel another caller's refresh, and failed or panicking providers remain redacted.
- **Security, resource, compatibility, and wire consequences:** Security supports rotation without global secret retention; resource work is bounded to one refresh cohort; compatibility follows AWS SDK v2 provider semantics; wire tokens never outlive their validated credential lifetime.
- **Executable evidence and public surface:** `TestProviderRefreshesExpiringCredentialsAndCapsTokenExpiry`, `TestConcurrentNearExpiryCredentialsRefreshOnce`, and `TestCallerCancellationIsNotSharedWithRefreshCohort` cover `Config`, `Provider`, and credential refresh behavior.
- **Upstream record and reconsideration:** The upstream record is AWS SDK for Go v2 v1.43.0 and its credential-provider APIs. Reconsider when SDK cache or credential-expiry semantics change.

## KAFKA-MSKIAM-DEC-003: Managed-service support requires direct evidence

- **Status, owner, and classification:** `resolved`; Kafka MSK IAM adapter maintainers; compatibility and support policy.
- **Source and issue:** The [Amazon MSK IAM client guidance](https://docs.aws.amazon.com/msk/latest/developerguide/configure-clients-for-iam-access-control.html) describes protocol setup, but local signer success does not prove authentication, authorization, transactions, replay, failure recovery, or lifecycle behavior on a real MSK mode and version.
- **Credible interpretations and known peer behavior:** The package could infer all MSK support from AWS's signer, claim Kafka protocol equivalence, skip unavailable live checks, or publish only exact retained live evidence. Client libraries often advertise managed-service compatibility after a connection smoke test.
- **Selected behavior:** Provisioned and Serverless MSK remain unverified until the fail-closed operator gate runs with bounded explicit inputs and retains its report. Local conformance proves signer, SDK, validation, and configuration behavior only; it does not upgrade the managed-service support matrix.
- **Security, resource, compatibility, and wire consequences:** Security and authorization claims require direct IAM evidence; resource and quota behavior require measured service evidence; compatibility remains an explicit non-claim; wire behavior is accepted only when the live broker completes the exercised matrix.
- **Executable evidence and public surface:** `TestMSKCompatibilityConfigRejectsUnboundedInputs`, `TestMSKControlPlaneValidation`, and `TestAmazonMSKCompatibility` cover the fail-closed compatibility gate and its report contract.
- **Upstream record and reconsideration:** The upstream record is each exact MSK mode, Kafka version, AWS guidance snapshot, and retained compatibility report. Reconsider only after direct content-addressed evidence is reviewed.

## Unresolved decisions

None. Amazon MSK modes without retained direct evidence are explicit
unsupported profiles, not unresolved interpretations.
