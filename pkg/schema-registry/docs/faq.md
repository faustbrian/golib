# FAQ

## Why are provider IDs and fingerprints separate?

Provider IDs are meaningful only inside one provider scope. The fingerprint is
derived from canonical content and portable references, so it can detect the
same schema across registries without impersonating either registry's ID.

## Why can successful registration return unknown?

Confluent and Glue can idempotently return an existing identity after a
concurrent request. Their response does not prove which caller created it.

## Does stale mode fail open?

No. It returns only an already validated, still-eligible entry and exposes the
upstream failure. It never registers or decodes against an unknown schema.

## Can decoding fetch references?

No. References are resolved while building a bounded graph, configuring a local
format adapter, or loading a bundle. Value decoding receives a compiled schema.

## Are all Confluent-compatible products identical?

No. Verify product/version behavior, extensions, authentication, quotas,
compatibility, and deletion with the provider integration suite.
