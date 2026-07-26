# Contributing

Preserve the authenticated persistence format, mandatory non-secret context,
bounded inputs, redacted diagnostics, and exact production coverage. New key
providers must transfer ownership of plaintext data-key bytes to the service
and must not log provider requests or responses.

Run the focused module gates in `make check`. Mutation testing is a release
gate and may be deferred only by an explicit project decision.
