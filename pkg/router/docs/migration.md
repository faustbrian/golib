# Migration

## From `http.ServeMux`

Split each pattern into `Route.Methods`, `Route.Host`, and `Route.Path`, retain
the existing handler, and check returned registration and compile errors.
`Request.PathValue` remains unchanged. Decide whether automatic OPTIONS and
custom 404 or 405 handlers are desired. See `compatibility.md` for deliberate
differences.

Methodless patterns must become an explicit bounded method list, and method
tokens must be uppercase. Replace literal or encoded dot-segment patterns with
semantic paths. Keep IP literals, ports, and application-selected IDNA
normalization at the server boundary rather than in route hosts. If existing
tables exceed `DefaultLimits`, raise only the measured budget before
registration.

## From Laravel routes

Translate groups and names, but construct dependencies before registration and
close over them in ordinary handlers. There is no controller resolution, model
binding, request validation, session, CSRF view system, container, facade, or
implicit authorization. Keep those concerns in explicit application code and
middleware.

## From third-party Go routers

Replace router-specific parameter access with `Request.PathValue`; replace
regex patterns with literals and standard wildcards; replace middleware name
registries with actual functions; and replace reverse-routing interpolation
with `Param` or `Remainder`. Unsupported regex constraints should be validated
inside a handler or at the application boundary. Compatibility with another
router's precedence or redirect quirks is not implied.
