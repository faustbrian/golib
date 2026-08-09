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
