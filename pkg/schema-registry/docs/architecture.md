# Architecture

The core owns values, policy validation, bounded concurrency, registration
single-flight, caching, bundles, reference-graph validation, and codec/framer
composition. Format packages own parsing and canonicalization. Provider modules
own remote identity, endpoint, authentication, retry, lifecycle, compatibility,
and deletion semantics. Wire framers are separate from both business codecs and
registry access.

No package uses global registration or service location. A compiled schema,
bundle, and parsed frame perform no network I/O. The only network-capable
interfaces are explicit provider and reference resolver calls receiving a
`context.Context`.

Locks protect only local maps and flight state. They are not held across
provider I/O or observer callbacks. Synchronous flight leaders avoid orphaned
background work; waiters can cancel independently.
