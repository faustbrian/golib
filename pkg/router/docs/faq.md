# FAQ

**Is this a framework?** No. It composes `http.Handler` values and owns no
application lifecycle or dependency graph.

**Where are route parameters?** In `Request.PathValue`, including host wildcard
values set by the router.

**Why no regex routes?** Standard patterns cover owned-service needs without a
regex denial-of-service or opaque precedence surface.

**Can routes change at runtime?** No. Build and compile a replacement router
under application-owned rollout policy if dynamic configuration is required.

**Are forwarding headers used for URLs?** Never. Supply an explicit validated
`BaseURL` after applying trusted proxy policy outside the package.

**Who recovers panics?** Explicit middleware. The router only contains
guards that convert controlled standard-library registration errors. Runtime,
handler, and middleware panics propagate unchanged.
