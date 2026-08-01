# Compatibility

The adapter supports AWS Secrets Manager string and binary secret values that
contain one JSON object. It uses the AWS SDK v2 `GetSecretValue` contract and
the current `golib/config` document model.

Adding optional `Options` fields is compatible. Changing the default version
stage, source priority, JSON object requirement, missing-secret mapping, or
error redaction is behaviorally breaking and requires a major release.
