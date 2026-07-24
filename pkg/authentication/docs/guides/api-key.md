# API keys

`apikey.NewStatic` stores fixed-size key ID and secret digests, compares the
complete active set, and returns principals with method `api_key`.
`apikey.New` adapts a database or secret-service validator.

The HTTP adapter requires separate ID and secret names. Enable exactly one of
`APIKeyHeader`, `APIKeyQuery`, or `APIKeyCookie`; headers are preferred because
URLs are commonly retained by proxies and analytics. Treat the ID as routing
metadata, not authentication by itself.

For rotation, call `Static.Replace` with a complete new set. Validation builds
the candidate set first and publishes it atomically, so a failed replacement
leaves the previous keys active. See `apikey.ExampleStatic_Replace`.
