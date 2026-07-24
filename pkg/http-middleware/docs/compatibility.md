# Compatibility

The public contract is Go 1.26.5 `net/http`. API snapshots are checked against
`api/baseline.txt`. Before v1, incompatible changes require changelog and
migration notes; after v1 they require a new major version.

HTTP/1.1 and HTTP/2 real-listener tests cover final responses, trailers,
informational responses, flushing, and protocol-specific hijacking. HTTP/3 is
not directly implemented;
generic HTTP semantics remain transport-neutral, but deployments must validate
their own HTTP/3 server adapter.

The package uses `httpsnoop` to preserve the exact standard optional interface
set on tracking/header wrappers. Buffered compression and timeout replay are
documented exceptions. Deprecated `CloseNotifier` can be preserved by the
underlying helper but is not part of this package's public promise.

No unsafe, cgo, `go:linkname`, reflection discovery, or runtime patching is
used. The root module does not require sibling owning packages.
