# Threat model and resource budgets

Attackers can control request methods, targets, transport headers, bodies,
origins, forwarding fields, encoding preferences, media ranges, timing, and
disconnect behavior. Trusted proxy operators and constructor callers are
privileged but may misconfigure policy.

| Resource | Default or hard bound |
|---|---|
| chain depth | 256 |
| descriptor name | 128 bytes |
| request identifier | 128 bytes default, 1024 hard maximum |
| proxy hops | 16 default, 128 hard maximum |
| trusted proxy prefixes | 256 hard maximum |
| parsed policy header | 8192 bytes default, 1 MiB hard maximum |
| CORS values | 64 default, 256 hard maximum |
| configured content types | 64 default, 256 hard maximum and header-byte budget |
| compression exclusions | 64 values, 256 bytes each |
| compression buffer | 1 MiB default, 16 MiB hard maximum |
| timeout response buffer | caller required, 16 MiB hard maximum |
| handler deadline or timeout | 24 hours hard maximum |
| retained timeout handlers | 1024 default, 65,536 hard maximum |
| admission wait | 1 minute hard maximum |
| route metadata | 128 bytes |
| client class | 64 bytes |
| observed method/protocol | fixed known classes plus `OTHER` |
| recovery stack | 64 KiB default, 1 MiB hard maximum |
| in-flight permits/waiters | caller configured, 1,000,000 hard maximum |

Mitigations cover CRLF splitting, spoofed forwarding fields, host confusion,
CORS cache poisoning, middleware-order bypass, panic disclosure, unbounded
draining, compression memory retention, wait queues, hostile policy slices,
and observer cardinality.
Ingress slowloris protection, HTTP request smuggling prevention, TLS, and total
header size remain server/proxy responsibilities.
