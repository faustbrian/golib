# Secure adoption checklist

1. Choose exactly one explicit credential source per route. Prefer dedicated
   headers; do not introduce query credentials in new designs.
2. Terminate TLS before credentials cross an untrusted network and configure
   proxies and access logs to omit authorization, cookies, and query strings.
3. Set callback, HTTP-client, request, refresh, token, claim, key, and shutdown
   bounds for the deployment's latency budget.
4. Configure exact JWT/OIDC issuer, audience, algorithms, clock, and skew. Use
   nonce validation for interactive OIDC flows.
5. Keep the authentication result separate from authorization policy. Check
   permissions, ownership, and tenant boundaries after authentication.
6. Emit only bounded outcome metadata through `authlog` or `authotel`; never
   add credentials, claims, principals, key data, or URLs.
7. Rotate with a brief overlap, unique key IDs, issuer cache awareness, and a
   tested rollback. Close every owned `jwt.Remote` with a deadline.
8. Run the protocol, race, fuzz, vulnerability, compatibility, coverage, and
   reproducibility gates documented in
   [security/test-matrices.md](security/test-matrices.md).
