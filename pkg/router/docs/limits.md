# Resource Limits

`DefaultLimits` supplies a complete positive construction budget. Callers may
replace it with `WithLimits`; every field must remain positive. Limits are
checked before a compiled router is published and generation limits are checked
before a URL is returned.

| Budget | Default | Applies to |
| --- | ---: | --- |
| Routes | 1,024 | Flattened routes and mounts |
| Groups | 64 | All nested and sibling group callbacks per root builder |
| Group nesting | 8 | Active nested groups |
| Methods per route | 16 | Explicit method set |
| Method bytes | 32 | Each HTTP method token |
| Wildcards per route | 16 | Host and path wildcard total |
| Wildcard name bytes | 64 | Each host or path wildcard identifier |
| Pattern bytes | 2,048 | Path pattern, composed group prefix, or mount pattern |
| Host bytes | 255 | Route or group host pattern |
| Name bytes | 128 | Group and route names, middleware identifiers, and exclusions |
| Source bytes | 256 | Diagnostic source label |
| Operation bytes | 128 | Operation identifier |
| Documentation bytes | 4,096 | Route or mount documentation |
| Metadata entries | 32 | Resolved route metadata |
| Metadata key bytes | 64 | Each caller metadata key |
| Metadata value bytes | 256 | Each caller metadata value |
| Middleware depth | 32 | Route middleware, exclusions, and resolved chain |
| Request target bytes | 8,192 | Raw and escaped path plus query during dispatch |
| URL parameters | 32 | Inputs and total remainder segments per generation call |
| URL parameter bytes | 4,096 | Names and raw values in one generation call |
| Query values | 128 | Values across all query keys |
| Query bytes | 4,096 | Raw keys and values before encoding |
| Generated URL bytes | 8,192 | Final relative or absolute URL |

The trusted authority passed to `NewBaseURL` is independently bounded to 261
bytes: a 255-byte hostname plus a colon and five decimal port digits. Scheme is
restricted to `http` or `https` and rejected before normalization when longer
than five bytes.

`Remainder` has an independent hard ceiling of 65,536 supplied segments so its
defensive copy is bounded before a compiled router's configured generation
limits are available. Generation still applies the lower configured URL
parameter and output budgets.

Registration and request validation are bounded by these inputs. A request
method longer than its configured method budget is rejected with 400, and a
request target beyond its budget is rejected with 414 before matching.
Compilation performs
pairwise conflict checks over the flattened route table and therefore uses
quadratic startup time in the configured route limit. Dispatch has no route
mutation or discovery. `Routes` deliberately copies the full route table and
its nested slices and metadata, so its work and allocation size are linear in
the published table. Applications should cache derived documentation instead
of calling it on every request.

Each group child receives only the parent's remaining route and group budget.
An exhausted group count is rejected before another callback runs, and route
registration inside a group cannot consume capacity already used by its
parent.
