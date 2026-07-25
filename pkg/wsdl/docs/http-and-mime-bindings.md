# HTTP and MIME bindings

WSDL 1.1 HTTP support models binding verbs, operation locations,
URL-encoded/URL-replacement input, and endpoint addresses. MIME support models
content media types, XML parts, and multipart/related groups. Validation checks
HTTP tokens, absolute endpoint addresses, media type syntax, and referenced
message parts.

WSDL 2.0 HTTP adjunct support includes default and per-operation method,
location templates, input/output/fault serialization, query separators,
content and transfer coding, cookies, ignore-uncited behavior, status codes,
typed headers, endpoint authentication properties, and the predefined
`wsdlx:safe` interface-operation property. Safety retains explicit `true` and
`false` presence separately from the absent default.

The model does not interpret URI templates, send HTTP traffic, select
credentials, or serialize application bodies.
