# FAQ

## Why create before putting?

AWS Secrets Manager separates secret-container creation from version creation.
The adapter attempts creation and falls back to `PutSecretValue` only when the
container already exists.

## Why require a stage?

AWS moves `AWSCURRENT` when `PutSecretValue` has no explicit stage. A unique
stage prevents a late historical write from changing a shared label.

## Does an exact retry create a duplicate?

No. AWS treats the same `ClientRequestToken` and the same value as idempotent.
It rejects reuse of the token with different material.

## Does the adapter read or compare secret values?

No. It exposes no read operation. AWS enforces immutable token semantics.

## Does it manage rotation or deletion?

No. Those are separate lifecycle policies and require their own authorization,
retention, and operational contracts.
