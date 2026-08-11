# Amazon MSK and ECS deployment guidance

Amazon MSK Provisioned and Serverless are currently **unverified and
unsupported** by this repository. This guide describes the intended deployment
boundary and the checks required before a support claim. It does not replace a
direct controlled-environment compatibility run.

The root Kafka module remains AWS-independent. Only
[`adapters/mskiam`](../adapters/mskiam) imports the AWS SDK and AWS-supported Go
signer.

## Select the MSK connection profile

Record whether the target is Provisioned or Serverless, its Kafka version where
applicable, Region, cluster ARN and ID, VPC connectivity, authentication modes,
encryption settings, and broker quotas. Obtain the matching bootstrap broker
string from MSK; do not construct endpoints or reuse a plaintext/SCRAM endpoint
for IAM.

Use normal MSK TLS bootstrap brokers for TLS, mTLS, or SCRAM according to the
cluster configuration. Use `BootstrapBrokerStringSaslIam` only when IAM access
control is enabled. The package requires verified TLS for every authenticated
mode.

Before application rollout, verify from the workload network:

- DNS resolution for every returned broker endpoint;
- security-group, route, subnet, and network ACL reachability;
- verified TLS hostname and trust-chain negotiation;
- the expected cluster identity and topic metadata;
- topic replication, ISR, retention, compaction, and
  `min.insync.replicas`; and
- authentication plus authorization for each actual producer, consumer,
  transaction, and inspection operation.

MSK bootstrap discovery and infrastructure mutation remain deployment or
operator responsibilities. Do not call AWS control-plane APIs from the root
Kafka package.

## IAM adapter construction

The default AWS SDK v2 chain is the preferred ECS path:

```go
provider, err := mskiam.New(ctx, mskiam.Config{
    Region:       "eu-north-1",
    TokenTimeout: 5 * time.Second,
})
if err != nil {
    return err
}

producer, err := kafka.NewProducer(kafka.ProducerConfig{
    Brokers:       iamBootstrapBrokers,
    ClientID:      "orders-producer",
    AllowedTopics: []string{"orders"},
    Security: kafka.ClientSecurity{
        Authentication: kafka.NewOAuthBearerAuthentication(provider),
    },
})
```

The adapter asks AWS's supported Go signer for a fresh token for each SASL
session. It bounds credential retrieval/signing, caps token expiry at the
signing credential expiry, and fails closed on nearly expired credentials or
tokens. It performs no proactive refresh and starts no goroutine.

Do not enable the signer's process-wide credential-debug mode. Do not log AWS
credential-provider causes, signed tokens, broker URLs, or task credential
endpoints.

## ECS task identity

Use an ECS **task role** for application permissions. The task execution role
is for ECS agent operations such as pulling images and delivering logs; it is
not the application's MSK identity.

ECS supplies task-role credentials through the container credential provider.
The AWS SDK v2 default chain reads the ECS-provided relative credential URI and
refreshes expiring credentials. Do not put access keys or session tokens in the
task definition, environment files, secrets copied into the image, or shared
instance profiles.

Use a separate least-privilege task role per service or task definition. On ECS
EC2, containers and the host do not form a hard security boundary; prevent
access to instance metadata and credentials belonging to other tasks according
to ECS guidance. Fargate provides a stronger task isolation boundary but does
not remove the need for least privilege.

The application container must be able to reach the ECS credential endpoint
and the MSK brokers. A network policy that blocks the credential endpoint can
look like Kafka authentication failure even when broker networking is healthy.

## IAM least privilege

MSK IAM uses IAM for both authentication and Kafka authorization. A valid token
does not grant cluster access. Scope `kafka-cluster` actions to the exact
cluster, topic, group, and transactional-ID resource ARNs required by the
application.

Review permissions by role:

| Client role | Resource decisions |
| --- | --- |
| producer | connect to the exact cluster; describe and write only allowed topics; grant the cluster-level idempotent-write action and the transactional-ID resources required by AWS's IAM implementation |
| consumer | connect; describe/read allowed topics; describe/alter only its group identities |
| transactional producer/processor | producer/consumer permissions plus describe/alter only its transactional-ID identities |
| inspector | only the cluster, topic, group, and configuration describe actions required by enabled probes |

Do not grant `kafka-cluster:*` to diagnose a failed login. Test denial as well
as success. Topic allowlists inside the package are application policy and do
not replace IAM authorization.

The exact action set depends on the enabled Kafka APIs and MSK resource model;
derive and review it from AWS's current IAM action and resource documentation
for the target cluster. Keep infrastructure policy tests outside this module.

Do not infer idempotent-production authorization from topic-level write access.
AWS documents `kafka-cluster:WriteDataIdempotently` as a cluster-level action
and requires access to transactional-ID resources for its IAM implementation.
Granting `WriteData` to every topic has different consequences from limiting it
to selected topics, so validate both the allowed and denied topic boundaries
with the final policy.

IAM transaction support is also broker-version-sensitive. AWS documents
`WriteTxnMarkers` support through IAM access control only for Kafka 3.8 or
later. On earlier broker versions, IAM cannot authorize the internal action
needed to terminate transactions; use SCRAM or mTLS with appropriate Kafka
ACLs instead. `AlterTransactionalId` permission alone does not make an IAM
transaction profile viable. A direct compatibility run must therefore reject
or mark transactions unsupported for IAM-authenticated brokers earlier than
3.8 rather than treating a successful token or ordinary produce as transaction
evidence.

## ECS deployment lifecycle

1. Validate Kafka, MSK IAM adapter, Region, topic, group, transaction, deadline,
   and buffer policy during task startup.
2. Construct the IAM provider once per owned client configuration. Let the AWS
   SDK credential cache refresh task credentials; do not install a process-wide
   mutable credential singleton.
3. Require bounded dependency readiness before accepting traffic, but keep
   broker dependency failures out of liveness.
4. During rolling deployment, give each transaction owner a unique task- or
   instance-scoped transactional ID. Do not use an ECS service name alone.
5. On SIGTERM, stop admissions, join consumers/processors, drain producers, and
   handle every close error inside the ECS stop timeout.
6. Make the configured shutdown bound shorter than the platform stop timeout
   with enough margin to report failure.

Credential rotation is automatic only when the selected provider and SDK cache
can refresh. The adapter requests current credentials for each authentication
session, but an established Kafka connection sees a new identity only after
reauthentication or reconnect.

## Provisioned and Serverless distinctions

Do not transfer a Provisioned compatibility result to Serverless or the
reverse. Record and test separately:

- available authentication modes and bootstrap broker forms;
- supported Kafka APIs and version-dependent behavior;
- partition, throughput, connection, request, and storage quotas;
- topic and broker configuration visibility;
- KRaft and group-protocol behavior exposed by the service;
- maintenance, scaling, failure, and endpoint behavior; and
- producer, consumer, transaction, replay, inspection, and credential-rotation
  scenarios.

Describe an unexecuted mode as unverified, even when franz-go and the signer
support the required protocol.

## Required direct compatibility run

Before claiming support, execute from the intended ECS launch type and Region:

1. exact cluster and bootstrap identity capture;
2. verified TLS and IAM token acquisition through the task role;
3. idempotent all-ISR single, batch, and asynchronous production;
4. cooperative consumer assignment, durable handling, offset commit,
   redelivery, and rolling replacement;
5. transaction commit/abort, fencing, read-committed visibility, and
   consume-transform-produce where the service supports it;
6. exact replay planning/execution and inspection within MSK permissions;
7. task-role credential refresh during broker reauthentication or reconnect;
8. IAM denial for topic, group, transactional-ID, and inspection boundaries;
9. broker endpoint interruption, maintenance or scaling event, and bounded
   recovery; and
10. race, leak, allocation, latency, throughput, shutdown, and raw evidence
    capture with exact versions and quotas.

Record Provisioned and Serverless results independently in the compatibility
matrix. A successful signer unit test is not broker evidence.

## Troubleshooting MSK IAM on ECS

| Failure boundary | Check without disclosing secrets |
| --- | --- |
| credential load | task role attached, container credential URI present, endpoint reachable, SDK selected source |
| token generation | Region, credential expiry, bounded provider category, task clock |
| TLS connection | IAM bootstrap endpoints, DNS/VPC path, system roots, hostname |
| authentication | IAM access control enabled, signer/adapter version, token not near expiry |
| authorization | exact task-role policy, cluster/topic/group/transactional-ID ARN and action |
| rotation | new task credentials retrieved, Kafka session reauthenticated or reconnected |

Do not print the credential endpoint response or enable credential debug output.
Use CloudTrail and ECS credential audit metadata according to operator policy;
keep those control-plane diagnostics out of Kafka record telemetry.

Primary AWS references:

- [How IAM access control for Amazon MSK works](https://docs.aws.amazon.com/msk/latest/developerguide/how-to-use-iam-access-control.html)
- [Configure clients for IAM access control](https://docs.aws.amazon.com/msk/latest/developerguide/configure-clients-for-iam-access-control.html)
- [IAM action and resource semantics](https://docs.aws.amazon.com/msk/latest/developerguide/kafka-actions.html)
- [Create IAM authorization policies](https://docs.aws.amazon.com/msk/latest/developerguide/create-iam-access-control-policies.html)
- [AWS SDK credential provider chain](https://docs.aws.amazon.com/sdkref/latest/guide/standardized-credentials.html)
- [Container credential provider](https://docs.aws.amazon.com/sdkref/latest/guide/feature-container-credentials.html)
- [Amazon ECS task IAM roles](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-iam-roles.html)
