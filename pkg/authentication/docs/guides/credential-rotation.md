# Credential and key rotation

For static API keys, construct the complete overlap set and call
`Static.Replace`. Keep old and new IDs deterministic and distinct. Confirm the
new credential is in use before removing the old entry.

For Basic credentials, construct and deploy a new immutable authenticator with
overlapping entries. For opaque bearer callbacks, make the backing store accept
both values during the window.

JWT remote caches refresh automatically within configured bounds and expose a
synchronous `Refresh` for controlled rollout. OIDC refreshes synchronously on
an unknown key ID once cached freshness and the refresh cooldown permit it. A
fresh issuer response can therefore delay discovery of a new key until its
bounded expiry. Never reuse a key ID for different material during an
overlap window. Preserve cached known keys during transient issuer outages but
fail unavailable for unknown keys.

Use the injected clock in deterministic rollout tests. Exercise current,
previous, newly introduced, revoked, and unknown keys, including issuer outage
and cache-expiry transitions.
