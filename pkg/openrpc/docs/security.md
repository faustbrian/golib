# Security model

All JSON inputs are untrusted. `jsonvalue` rejects invalid UTF-8, duplicate
object names, trailing data, excessive bytes, depth, and tokens before a value
enters the model. Parse, pointer, expression, validation, diff, resolver, and
composition policies add operation-specific finite limits. Errors and
diagnostics identify safe codes and pointers without including complete
documents, fetched bodies, credentials, or provider errors.

Core parsing and validation never access the network or filesystem. Reference
resolution is disabled externally by default. Enabling it requires all of:

1. an explicit `reference.Store`;
2. `AllowExternal`;
3. an allowed scheme; and
4. an allowed host.

Resolver work is bounded by depth, aggregate references, loaded documents,
fetched bytes, JSON bytes, JSON tokens, pointer length, and pointer tokens.
The aggregate reference limit applies before allocating direct input results
and across aliases, bundle roots, and transitive resource-graph fan-out.

Draft 7 compilation accepts only caller-supplied resources and limits both
resource count and aggregate schema bytes before compiler registration. The
defaults allow 1,024 resources and 64 MiB across the root and resources.

The memory store owns its input. The `fs.FS` store rejects scope escape,
encoded paths, fragments, queries, credentials, and traversal. The optional
HTTP store requires exact hosts, uses HTTPS unless HTTP is explicitly enabled,
resolves and checks every address before dialing, blocks private and special
address classes by default, disables transparent decompression, rejects content
encodings, bounds redirects, headers, dial and request time, and streams through
the caller's remaining byte allowance.

Callers remain responsible for choosing trustworthy allowlisted hosts and
stores. Allowing private addresses or HTTP intentionally weakens the SSRF and
transport boundary and should be limited to controlled development networks.

Runtime expressions implement JSON value lookup only. They define no loops,
operators, callbacks, reflection, or code execution. Whole expressions preserve
JSON types; objects and arrays cannot be interpolated into surrounding text.

Discovery validates provider and filter output under explicit diagnostic and
canonical-byte limits. Oversized generated documents fail before a snapshot is
published or returned through the JSON-RPC adapter.

Semantic validation checks generated document method counts before copying or
traversing the method collection. Parser limits therefore are not the only
defense for code-first documents.

The production module does not use `unsafe`, cgo, `go:linkname`, background
network fetches, global mutable registries, or telemetry exporters.

## Reporting vulnerabilities

Do not open a public issue for a suspected vulnerability. Send a private report
to the repository owner with the affected version, a minimal reproducer, impact,
and any proposed mitigation. Avoid including credentials or production data.
