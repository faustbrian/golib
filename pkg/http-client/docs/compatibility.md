# Compatibility Policy

The module follows semantic versioning. Before v1, minor releases may adjust
exported APIs with documented migration notes; patch releases remain focused on
compatible fixes. At v1, removing or changing an exported type, function,
method, interface requirement, constant meaning, or sentinel matching contract
requires a major release.

Compatibility also includes observable policy: default timeouts and limits,
middleware order, header names, retry classifications, redirect credential
behavior, response ownership, cache semantics, profile values, telemetry label
sets, fixture schema, error rendering, and whether errors match a sentinel.
Security fixes may deliberately tighten rejected input or trust boundaries and
will be called out prominently.

Named profiles and fixture schemas carry explicit major versions. New optional
fields may be added compatibly, but persisted fixture readers reject unknown
current-schema fields and require explicit migration for old schemas. Exported
interfaces are kept narrow because adding a method is breaking for external
implementations.

The supported Go version is the version declared in `go.mod`. A change to that
minimum is announced in the changelog. HTTP/3 is not part of v1; adding it
requires separately proven fallback, telemetry, transport ownership, and
compatibility behavior.
