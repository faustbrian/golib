# Security model

## Trust boundaries

Schemas, authorization callbacks, mandatory constraints, persistence mappings,
cursor keys, clocks, and replay storage are trusted server configuration.
Requests, query strings, JSON-RPC parameters, JSON:API families, cursor tokens,
and all filter values are hostile input. A plan is reviewed only after
`apiquery.Compile` succeeds.

## Enforced controls

- Capability names and operations come from immutable schema allowlists.
- Authorization is evaluated per field, filter, sort, and relationship edge.
- Required execution fields are separate from response projection.
- Tenant and policy constraints are server-owned and cannot be supplied,
  removed, shadowed, or replaced by a client filter.
- PostgreSQL mappings accept only validated identifiers and emit positional
  parameters for every value.
- Cursor payloads are authenticated during plan compilation. They use
  AES-256-GCM and bind protocol version, key ID, schema
  revision, exact ordered sorts, direction, typed positions, expiry, and policy.
- Cursor key rotation is atomic. Optional replay guards receive only an opaque
  SHA-256 fingerprint and expiry.
- HTTP and JSON-RPC reject malformed UTF-8, unknown members, duplicate members
  or parameters, invalid encoding, trailing data, and oversized input.
- Bounds cover fields, includes, relationship depth, filter depth and nodes,
  values, membership lists, strings, sorts, page size, offset, cursors,
  canonical output, error count, and conservative cost.
- Compiled plans replace raw cursor tokens with bounded typed state, and their
  canonical form contains only a deterministic cursor-state digest.
- Diagnostics use stable paths and sanitized messages. They do not include raw
  cursors, rejected values, signing keys, protected predicates, or inaccessible
  schema details.

## Threat matrix

| Threat | Control and evidence |
| --- | --- |
| SQL injection | Allowlisted identifiers, positional values, PostgreSQL 18 integration |
| Field exfiltration | Explicit fields, per-capability authorization, response/execution split |
| Relationship traversal | Declared edges, depth/count/cycle bounds, authorization per edge |
| Tenant escape | Separate immutable mandatory constraints, adapter fail-closed mapping |
| Cursor forgery | Authenticated encryption, exact schema/sort binding, size and TTL bounds |
| Cursor replay | Optional serialized replay guard with opaque fingerprints |
| Expensive queries | Conservative declared costs and structural bounds before adapters |
| Unicode confusion | ASCII capability grammar and strict UTF-8 transport parsing |
| Schema probing | Authorization failures are stable and intentionally nonspecific |
| Resource exhaustion | Bounded decode, compile, canonicalization, errors, and page sizes |

## Consistency model

Cursor traversal is seek-based over the exact total order in the cursor. It
prevents duplicates caused by inserts before an already consumed boundary and
continues after deleted boundary rows because positions, not row identity, are
used. It is not snapshot isolation: rows inserted after the boundary may appear
and updates to sort keys can move rows. Applications needing a fixed snapshot
must add a protected snapshot/version constraint and encode its policy identity
in the cursor.

## Operational requirements

Keep cursor keys outside source control, use unique nonsecret key IDs, cap TTL,
retain old decode keys only through the longest issued TTL, and retire them
afterward. Replay guards must bound retained fingerprints by expiry. Never log
`Value.String()` for protected values or raw cursor tokens.

Report vulnerabilities privately to the repository owner. Include the affected
version, minimal reproduction, and impact; do not include production secrets or
customer data.
