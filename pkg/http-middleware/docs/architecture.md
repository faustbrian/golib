# Architecture

Construction and serving are separate. Constructors validate and copy policy;
request handlers retain no mutable global configuration. The root `Chain` is a
slice of immutable descriptors resolved around one terminal handler from last
to first.

Subpackages depend only on `net/http`, narrowly shared internal response and
field helpers, and documented parsing dependencies. The root imports no
subpackage. `adapter` names middleware from owning packages but does not copy
their policy or state machines.

Short circuits own the response they emit. They use deterministic plain-text
bodies, `no-store`, `nosniff`, bounded status values, and never include wrapped
errors or caller data. Once downstream commits a response, recovery and limit
layers do not rewrite it.

There is no initialization registration, alias lookup, reflection discovery,
service location, default chain, exporter, logger, cache, refresher, or shutdown
hook.

`make architecture` rejects production imports of `reflect` or `unsafe`, cgo
and native source, linkname directives, package initializers, and dependencies
on owning sibling packages. It also runs the complete suite with cgo disabled.
