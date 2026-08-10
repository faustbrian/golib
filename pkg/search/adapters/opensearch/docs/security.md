# Security and authentication

Basic credentials and AWS request signing are mutually exclusive. Credential
providers are consulted for every request so rotation does not require client
replacement. The adapter rejects credentials over plaintext HTTP, endpoint
userinfo, implicit environment proxies, insecure TLS, and untrusted discovery
addresses.

The index resolver must authorize tenant, logical index, and access mode before
returning a physical target. Lifecycle methods additionally require the
lifecycle authorizer. Give runtime search credentials no destructive lifecycle
privileges when deployments can use separate clients or roles.

Backend bodies, queries, sources, signed cursors, credentials, and authorization
errors may contain secrets. Adapter failures expose bounded classifications and
metadata instead of echoing response bodies or provider errors.

The advisory review was refreshed on 2026-08-10. The OpenSearch repository's
published GitHub advisories list CVE-2023-31419 and CVE-2022-41917, both fixed
well before the supported 2.19.6 and 3.8.0 server versions. The 3.8.0 release
also upgrades dependencies for CVE-2026-8149, CVE-2026-54515, and
CVE-2026-2332, and hardens deserialization and filesystem path boundaries.
Operators must still review OpenSearch, plugin, JVM, image, and managed-service
advisories for their exact deployment before every release.

Primary references:

- <https://github.com/opensearch-project/OpenSearch/security/advisories>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/3.8.0>
