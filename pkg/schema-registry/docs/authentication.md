# Authentication and endpoints

The Confluent adapter requires HTTPS except for an explicit test-only option,
rejects URL userinfo, query strings, and fragments, and uses an injected
`RoundTripper` without following redirects. Its credential provider supplies an
Authorization value only for the configured endpoint. Use a narrowly scoped API
key or bearer credential and rotate it outside this package.

The Glue adapter receives an AWS SDK v2 client already configured with region,
endpoint policy, credentials, and SDK retries. Use the workload's region and
least-privilege IAM permissions for the exact registry/schema operations. Do
not forward AWS credentials to custom endpoints unless the application's
endpoint policy explicitly trusts them.

Never include credential values or complete schemas in errors, metrics, traces,
or diagnostics.
