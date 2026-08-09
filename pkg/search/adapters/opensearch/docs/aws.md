# AWS authentication and managed deployments

Use the official OpenSearch client's AWS SDK v2 signer. The AWS SDK credential
provider chain owns refresh and rotation; the adapter invokes the signer for
every request and does not cache credentials.

```go
awsConfig, err := config.LoadDefaultConfig(ctx,
    config.WithRegion("eu-north-1"),
)
if err != nil {
    return err
}
requestSigner, err := awsv2.NewSigner(awsConfig)
if err != nil {
    return err
}
client, err := opensearch.New(opensearch.Config{
    Endpoints:            []string{"https://vpc-domain.eu-north-1.es.amazonaws.com"},
    Signer:               requestSigner,
    RequestTimeout:       3 * time.Second,
    MaximumResponseBytes: 8 << 20,
})
```

For Amazon OpenSearch Serverless, construct the signer with the service name
required by that deployment using `awsv2.NewSignerWithService`; do not infer a
service or region from an untrusted endpoint. Use workload identity/role
credentials (IRSA, ECS task roles, EC2 roles, or an equivalent short-lived
provider) instead of static access keys.

IAM and domain policies should grant runtime clients only the specific HTTP
methods and aliases needed for search and externally versioned document writes.
Use separately constructed lifecycle credentials for templates, index create,
reindex, aliases, and delete. Network topology remains configuration: the
adapter does not assume public domains, VPC domains, PrivateLink, a fixed node
count, or AWS-managed discovery behavior.

Do not forward signed requests through an untrusted proxy. Discovery must be
disabled for managed endpoints unless returned publish addresses can be
explicitly allowlisted and reached without escaping the intended authority.
