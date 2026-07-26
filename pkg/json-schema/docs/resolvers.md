# Resolvers and secure loading

The core has no implicit network, environment, or filesystem access. A schema
that needs an unavailable non-local resource returns
`ErrResourceUnavailable`. Configure retrieval with `WithResourceLoader`.
The only built-in resource table is the checksum-pinned official meta-schema
bundle; its published URIs resolve locally before an application loader.

- `NewMapLoader` copies an in-memory registry and returns copied bytes.
- `NewFSLoader` maps one absolute base URI into a caller-provided `fs.FS` and
  rejects authority changes, queries, fragments, invalid paths, and traversal.
- `NewCompositeLoader` tries loaders in order and falls through only on
  `ErrResourceNotFound`.
- `ResourceLoaderFunc` adapts an application policy.

`MapLoader` and `FSLoader` normalize RFC-equivalent identifiers. Map
construction rejects two keys that normalize to the same resource, preventing
map iteration order from selecting an alias. Custom loaders receive the
normalized identifier chosen by reference resolution.

For OS directories, prefer `os.OpenRoot(...).FS()` on supported platforms and
retain/close the `os.Root` in application lifecycle code. A generic `fs.FS`
may have implementation-specific symlink or device semantics; the caller owns
that boundary.

HTTP retrieval is intentionally not built into the core. An application
adapter must enforce scheme and host allowlists, redirect rules, DNS and proxy
policy, authentication separation, TLS, timeouts, response byte limits, and
cache ownership before returning bytes. Never accept credentials embedded in
schema URIs. Package-generated resolution errors remove URI user information,
query parameters, and fragments.

Loader calls receive cancellation. Returned schema documents are still
subject to resource-count and aggregate schema-byte limits. Loader errors
remain available to `errors.Is` and `errors.As` beneath
`ErrResourceUnavailable`, while their text is redacted from package-generated
diagnostics.
Panics from a loader or from an `fs.FS` supplied to `FSLoader` are contained as
redacted `ErrCallbackPanic` errors.
