# Behavior Matrices

These tables summarize the v1 behavior frozen by the executable unit,
differential, security, fuzz, race, and property tests.

## Request outcomes

| Request condition | Outcome | `Allow` |
| --- | ---: | --- |
| Explicit method, host, and path match | Route handler | Unchanged |
| HEAD with matching GET and no explicit HEAD | GET route handler | Unchanged |
| Explicit OPTIONS match | OPTIONS route handler | Unchanged |
| Automatic OPTIONS on a known host and path | 204 | Sorted methods plus implied HEAD and OPTIONS |
| Other method on a known host and path | 405 | Sorted methods plus implied HEAD and OPTIONS |
| Valid method absent from the compiled table and no path match | 501 | Unset |
| No matching host and path | 404 | Unset |
| Invalid method or target | 400 | Unset |
| Request target beyond its configured byte budget | 414 | Unset |
| `OPTIONS *` with automation enabled | 204 | Compiled methods plus implied HEAD and OPTIONS |
| `OPTIONS *` with automation disabled | Explicit not-found handler or 404 | Unset |
| CONNECT registration or authority-form dispatch | Typed startup error or 400 | Unset |

An explicit OPTIONS route wins over automation. Disabling automatic OPTIONS
removes OPTIONS from generated `Allow` values unless it was registered.

## Registration and conflict outcomes

| Input relationship | Result | Publication state |
| --- | --- | --- |
| Empty or relative path, malformed wildcard, escape, or dot segment | `ErrInvalidRoute` | No route retained |
| Empty, lowercase, duplicate, oversized, or invalid method token | Typed validation or limit error | No route retained |
| CONNECT method | `ErrUnsupported` | No route retained |
| Duplicate stable name | `ErrDuplicateName` at compile | No router published; builder remains unfrozen |
| Equivalent or ambiguous `ServeMux` patterns | `ErrConflict` at compile | No middleware constructed and no router published |
| Equal-specificity overlapping host patterns | `ErrConflict` at compile | No middleware constructed and no router published |
| More-specific overlapping pattern | Accepted; standard specificity selects it | Complete immutable router published |
| Failed group callback or child validation | Original typed or callback error | No child route retained by the parent |
| Any configured count or byte budget exceeded | `ErrLimitExceeded` | Rejected before publication |
| Successful compile followed by registration or compile | `ErrCompileState` | Existing compiled router remains usable |

Routes are sorted before whole-table checks. Conflict acceptance, the returned
error category, dispatch, and published introspection therefore do not use
registration order as a tie-breaker.

## Pattern and precedence

| Dimension | Rule |
| --- | --- |
| Literal, `{name}`, `{name...}`, and `{$}` paths | Go 1.26 `http.ServeMux` grammar and specificity |
| Exact host versus wildcard host | Exact host wins |
| Wildcard host versus hostless route | Wildcard host wins for a matching authority |
| Request authority port | Removed before route-host matching |
| Different overlapping host signatures at equal specificity and method | Rejected conservatively, even for disjoint paths |
| Duplicate semantic route | Rejected regardless of registration order |
| Canonical or subtree redirect | Followed by default; structural escaped-path redirects become 404 under `RejectRedirects` |
| Canonical redirect on a route or method miss | Runs before 404, 405, or unsupported-method classification |
| Encoded slash | Remains inside one wildcard value rather than becoming a separator |

## Path-value and request state

| Match or outcome | `Request.PathValue` | Request URL and context |
| --- | --- | --- |
| Literal path | No new value | Preserved |
| `{name}` segment | One decoded segment | Preserved |
| Encoded slash in `{name}` | Decoded slash remains one value | Escaped request path remains structural input |
| `{name...}` remainder | Decoded matched remainder | Preserved unless an explicit mount strips a clone |
| `{name}` host label | One validated authority label | Port is ignored only for matching |
| GET route serving HEAD | Same values as GET | Original HEAD method preserved |
| 400, 404, 405, 414, 501, or automatic OPTIONS | No route metadata installed | Request and context are preserved; custom 404 and 405 handlers receive them |
| Nested router mount | Non-conflicting outer values inherited | Inner wildcard wins a same-name collision |

`MatchedRoute` is available only inside the selected middleware and handler
chain and returns a defensive `RouteInfo` copy.

## Composition

| Surface | Request order | Response order or merge rule |
| --- | --- | --- |
| Middleware | Router, outer group, inner group, route | Exact reverse unwind order |
| Metadata | Outer group, inner group, route | Duplicate keys are errors; no silent override |
| Names | Outer prefix, inner prefix, route name | Literal concatenation |
| Hosts | One identical non-empty value across layers | Different values are errors |
| Paths | Validated prefix joins without `path.Clean` | Empty and dot segments are errors |
| Failed group callback | No routes are published | Parent remains usable |

Named middleware exclusion is an explicit route field. Nil middleware,
duplicate resolved names, and a middleware layer returning a nil handler are
errors before serving.

| Middleware behavior | Contract |
| --- | --- |
| Normal chain | Request order is router, outer group, inner group, route; response order is reversed |
| Named exclusion | Removes matching inherited router or group middleware, never route-local middleware |
| Duplicate resolved name | Compile error before any handler graph is published |
| Nil middleware or nil returned handler | Typed error before serving |
| Short circuit | Downstream middleware and handler are not called |
| Panic during construction or serving | Propagates unchanged; router recovery is not implicit |
| Cancellation | Original canceled context reaches the selected chain |
| Re-entry | One compiled router may synchronously dispatch another request |
| Flush, hijack, push, or stream | Original `ResponseWriter` and optional interfaces are preserved |
| Custom 404 or 405 partial write followed by panic | Status, body, `Allow`, context, and panic remain handler-owned |
| Malformed request | Returns 400 before custom 404, custom 405, or route middleware |

## Mounts and generated URLs

| Surface | Contract |
| --- | --- |
| Mount boundary | One explicit remainder-wildcard route |
| Strip prefix | Uses a cloned request URL; preserves the caller URL, `RequestURI`, and escaped suffix across encoded literal prefixes |
| Nested router | Caller-owned handler; preserves outer path values, with inner names winning collisions |
| Relative path | Requires every path wildcard exactly once and rejects host parameters |
| Wildcard identifier | Accepts every Go identifier supported by `ServeMux`, including `_` and Unicode letters |
| Absolute URL | Requires an explicit validated base and every host and path wildcard exactly once |
| Segment parameter | Escaped as one URI path segment; structural slash injection is rejected |
| Remainder parameter | Non-empty explicit segment list; dot and empty segments are rejected |
| Query | `url.Values.Encode` ordering with bounded keys and values |
| Unknown, missing, duplicate, or unused parameter | Typed generation error |

Generated paths are property-tested by dispatching them through the compiled
router and comparing the resulting `Request.PathValue` values.

## URL-generation security outcomes

| Input | Result |
| --- | --- |
| Missing, duplicate, unknown, unused, or wrong-kind parameter | `ErrInvalidParameter` |
| Empty or dot segment | `ErrInvalidParameter` |
| Slash inside a segment | Percent-escaped once and round-trips as one path value |
| Existing percent escape text | Percent sign is escaped; no structural double decoding |
| Remainder | Each non-empty segment is independently escaped |
| Oversized remainder constructor | Fixed-ceiling sentinel without copying the caller slice |
| Non-HTTP scheme, user info, malformed port, control byte, or non-ASCII host | Typed generation error |
| Host wildcard containing a separator or extra label | `ErrInvalidParameter` |
| Query separators, Unicode, or control text | Encoded by `url.Values.Encode` within configured budgets |
| Extra trusted base port with a route host | Explicit port retained with the rendered route authority |
| Generated path or URL over its output budget | `ErrLimitExceeded`; no partial string returned |
