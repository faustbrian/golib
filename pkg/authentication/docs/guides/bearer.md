# Opaque bearer tokens

Use `bearer.New` with `bearer.ValidatorFunc` or a `bearer.Validator`
implementation. The callback receives a bounded token and must return a
principal whose method is `bearer`.

For small configured token sets, use `bearer.NewStatic`. It compares every
well-formed credential against every active token using keyed fixed-size
digests. Call `Static.Replace` with the complete desired set to atomically add
an overlap token or remove a retired token; failed replacements leave the
previous set active.

Return a classified rejection for an unknown or expired token. Return an
unavailable failure for a context-bounded database or introspection outage.
Never include the token in callback errors. Outbound OAuth token acquisition
and token issuance are outside this project.

See `bearer.ExampleNew`, `bearer.ExampleNewStatic`, and the HTTP quickstart.
