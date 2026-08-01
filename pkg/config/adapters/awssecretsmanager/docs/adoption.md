# Adoption

Use this adapter when an application must retrieve startup configuration
directly from AWS Secrets Manager using workload identity.

1. Store one JSON object whose keys match the destination `config` schema.
2. Grant the workload only `secretsmanager:GetSecretValue` for that secret.
3. Load the AWS SDK configuration through its default credential chain.
4. Put the source below process-environment overrides in an explicit plan.
5. Load once before constructing components and fail startup on invalid data.

Use the ordinary environment or filesystem sources when an operator, CSI
driver, or sidecar already materializes the secret. Do not fetch one secret per
field or use this adapter as a rotation controller.
