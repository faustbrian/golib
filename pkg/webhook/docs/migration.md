# Migration and SemVer

There is no legacy API before v1. Adoption should first deploy verification in
shadow observation mode using synthetic fixtures, then enforce signatures,
then enable a tenant-scoped replay store, and finally enable outbound retries
only after endpoint idempotency is proven.

Changing canonical fields, nonce handling, encodings, ordering, line endings, header grammar,
body digest, envelope wire bytes, exported error identities, retryable status
classification, or an existing provider preset requires a major version.
Adding an isolated algorithm or provider can be minor when negotiation cannot
downgrade existing behavior. Security fixes may intentionally reject input
that was previously accepted and will be called out in the changelog.
