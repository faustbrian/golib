# JSON:API composition

This project does not define JSON:API query syntax. Parse and validate JSON:API
parameters with `github.com/faustbrian/golib/pkg/jsonapi`; its names, extensions,
profiles, and recommendations remain authoritative. Then pass the parsed
`jsonapi.Query` to `apiqueryjsonapi.FromQuery`.

Sparse fieldsets and includes map only through the configured resource. Filter
and page parameter families require explicit `FilterDecoder` and `PageDecoder`
callbacks because JSON:API does not assign one universal semantic grammar to
them. An unconfigured present family returns `ErrUnsupported`; callback errors
become sanitized `ErrInvalid`.

Do not feed a conventional HTTP query directly to this bridge and do not use it
to override JSON:API member naming or extension negotiation. After composition,
compile the ordinary transport-neutral request and compare its canonical plan
with equivalent JSON-RPC/HTTP requests in conformance tests.
