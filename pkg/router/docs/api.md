# API Reference

## Construction

- `New(options ...Option) *Builder` creates single-owner startup state.
- `Builder.Register(Route) error` validates and copies a descriptor.
- `Builder.Group(GroupOptions, func(*Builder) error) error` transactionally
  flattens nested routes.
- `Builder.Mount(string, http.Handler, MountOptions) error` adds an ordinary
  handler below a bounded remainder route.
- `Builder.PendingRoutes() []Route` returns defensive construction snapshots.
- `Builder.Compile() (*Router, error)` publishes immutable dispatch state.

`Route` contains its optional stable name, method set, host and path patterns,
handler, ordered middleware, explicit inherited exclusions, metadata,
documentation, operation identifier, and bounded source label. `GroupOptions`
composes the corresponding group-owned values. `MountOptions` controls the
mount boundary, methods, metadata, middleware, and request-path stripping.

## Options and limits

`WithLimits`, `WithMiddleware`, `WithNotFound`, `WithMethodNotAllowed`,
`WithAutomaticOPTIONS`, and `WithRedirectPolicy` are construction options.
`DefaultLimits` returns a complete positive `Limits` value. `FollowRedirects`
preserves `ServeMux`; `RejectRedirects` turns canonicalization matches into
not-found behavior.

The default values, including request-target, documentation, and
trusted-authority byte budgets, are listed in [Resource Limits](limits.md).

## Serving and inspection

`Router` implements `http.Handler`. `Router.Routes` returns ordered copied
`RouteInfo` values without handlers or function identities. `MatchedRoute`
returns a copied `RouteInfo` from the active request context. Parameters remain
in `Request.PathValue`.

`RouteInfo` exposes name, explicit methods, host, path pattern, wildcard names,
resolved middleware identifiers, copied metadata, documentation, operation,
and source. `NamedMiddleware` pairs a visible name with the standard
`Middleware` function shape.

## Generation

`Param` creates a single path-segment or host-label `URLParameter`.
`Remainder` creates an explicit segment list. `Router.Path` generates a
relative path. `NewBaseURL` validates an immutable trusted HTTP authority and
`Router.URL` generates an absolute URL with deterministic `url.Values` query
encoding. Parameter names follow the complete `ServeMux` Go-identifier rule,
including leading underscores and Unicode letters; generation performs no
reflection or implicit identifier lookup.

## Errors

`Error` contains a stable kind, field, bounded source, and bounded detail. Its
`Unwrap` supports `errors.Is` against `ErrInvalidRoute`, `ErrConflict`,
`ErrDuplicateName`, `ErrInvalidParameter`, `ErrGeneration`, `ErrUnsupported`,
`ErrCompileState`, and `ErrLimitExceeded`. Use `errors.As` for structured
diagnostics; never parse error text. Rendered messages are single-line valid
UTF-8 with control characters replaced.

The Go documentation on every exported declaration is authoritative for exact
signatures. `make docs` verifies exported documentation and Markdown links.
