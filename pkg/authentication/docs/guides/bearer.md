# Opaque bearer tokens

Use `bearer.New` with `bearer.ValidatorFunc` or a `bearer.Validator`
implementation. The callback receives a bounded token and must return a
principal whose method is `bearer`.

Return a classified rejection for an unknown or expired token. Return an
unavailable failure for a context-bounded database or introspection outage.
Never include the token in callback errors. Outbound OAuth token acquisition
and token issuance are outside this project.

See `bearer.ExampleNew` and the HTTP quickstart.
