# Standard-library Compatibility

The minimum supported Go release and development toolchain are 1.26.5, the
latest stable release when this contract was written.

## Preserved behavior

`router` delegates supported pattern parsing, specificity, wildcard
extraction, escaped-segment matching, GET-to-HEAD matching, canonical-path
redirects, and subtree redirects to `http.ServeMux`. Compatibility tests run
the same request matrices through both handlers and freeze those results.
Canonicalization occurs before route and method selection, including for
cleaned destinations that ultimately become 404, 405, or an unsupported-method
miss.

Handlers use `http.ResponseWriter`, `*http.Request`, `http.Handler`, and
`Request.PathValue` directly. No request URL mutation, context replacement,
response-writer proxy, unsafe link, copied standard-library matcher, or hidden
registration is used.

## Deliberate differences

| Area | `http.ServeMux` | `router` | Migration impact |
| --- | --- | --- | --- |
| Registration | Panics for malformed or conflicting patterns | Returns typed bounded errors | Handle compile errors during startup |
| Route definition | Pattern string and handler | Explicit immutable descriptor | Move method, host, and path into fields |
| Methodless and mixed-case patterns | May omit a method or use any valid case-sensitive method token | Requires one or more explicit uppercase method tokens | Enumerate accepted methods and normalize method constants during migration |
| Encoded dot-segment patterns | Accepts percent-encoded `.` or `..` as structurally clean literal data | Rejects literal and percent-encoded dot route segments | Replace the pattern with a semantic literal and validate legacy data in the handler |
| Pattern host syntax | Supports literal pattern hosts | Accepts bounded ASCII DNS labels plus explicit single-label wildcards; ports and IP literals are not route patterns | Put IP, port, or IDNA normalization at the trusted server boundary |
| Input and table size | Does not provide mux-level budgets | Rejects descriptors, collections, nesting, request targets, generation input, and output beyond configured limits | Start with `DefaultLimits` and raise a measured field explicitly if needed |
| Default 404 | Standard-library response | Exact standard-library status, headers, and body | No change unless a custom handler is configured |
| Default 405 | Standard-library response | Exact standard-library response when automatic OPTIONS is disabled | Disable automation for an identical `Allow`, or account for OPTIONS |
| OPTIONS and `Allow` | An unregistered OPTIONS request is 405; `Allow` omits OPTIONS | Explicit OPTIONS wins; otherwise automatic 204 is enabled and `Allow` includes OPTIONS | Register OPTIONS, disable automation, or accept the advertised automatic method |
| `OPTIONS *` | Rejected with 400 | Automatic 204 with the compiled method set, or explicit not-found behavior when disabled | Route server-wide OPTIONS policy outside the mux when standard behavior is required |
| Unsupported method miss | Missing path is 404 | Valid method absent from the compiled table is 501; a known path remains 405 | Register the method anywhere or translate 501 in an outer handler if required |
| Host wildcards | Only literal host patterns | Validated single-label `{name}` host wildcards with explicit precedence | Use literal hosts to retain exact standard behavior |
| Host input | Relies on server-parsed authority | Rejects malformed, non-ASCII, user-info, and invalid-port authorities with 400 | Normalize trusted IDNA before dispatch and reject malformed proxy input |
| Redirect policy | Canonical and subtree redirects | Same by default; may explicitly turn them into 404 | Keep `FollowRedirects` for standard behavior |
| Introspection | No immutable public route table | Stable copied descriptors without handlers | Metadata can drive docs and policy adapters |
| URL generation | Not provided | Named, bounded, component-escaped generation | Supply every wildcard explicitly |
| Middleware | Caller wraps handlers manually | Visible router/group/route composition | Ordering is frozen at compile time |
| CONNECT | Authority-form may be matched by a method pattern | Registration is rejected in v1 | Mount CONNECT handling outside this router |

The 404 and 405 compatibility assertions use automatic OPTIONS disabled,
because enabling it deliberately changes both OPTIONS dispatch and the methods
advertised by 405. Executable divergence fixtures freeze the four affected
cases: 405 `Allow`, origin-form OPTIONS, `OPTIONS *`, and unsupported-method
misses.

The complete status, precedence, redirect, middleware, mount, and generation
tables are frozen in [Behavior Matrices](matrices.md).

The package converts only panics raised synchronously while it calls
`ServeMux.Handle` with package-produced patterns and only when the recovered
value is a non-runtime error, which is the standard library's controlled
registration failure shape. Runtime faults and panics from handlers,
middleware, or unrelated caller code are never converted or recovered.
