# Anonymous routes

Authentication is fail-closed by default. `authhttp.WithOptionalAnonymous`
allows the next handler to run anonymously only when every enabled source is
absent. A malformed, duplicated, rejected, or unavailable credential still
fails.

The middleware stores `AnonymousPrincipal` explicitly, so downstream code can
distinguish optional access from a missing middleware invariant. Never create a
partially populated principal to represent absence. Authorization must define
what anonymous callers may do.

See `authhttp.ExampleWithOptionalAnonymous`.
