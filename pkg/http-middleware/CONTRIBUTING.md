# Contributing

Use Go 1.26.5 and make focused changes. New behavior starts with a failing test
that proves the missing contract. Security decisions need boundary, malformed,
cancellation, panic, and ordering cases.

Run `make check` before submitting a change. Public APIs require documentation,
an API baseline update, changelog entry, compatibility analysis, and tests.
Parsing, validation, serialization, forwarding, CORS, response, or protocol
changes also require review of
[`docs/specification-decisions.md`](docs/specification-decisions.md), its pinned
source manifest, and linked conformance evidence. Preserve superseded decisions
instead of erasing their history.
Never weaken the 100% production statement coverage gate with generated or
test-only files. Do not add globals, hidden registration, unsafe, cgo,
reflection discovery, network calls, or background goroutines.

Commits use Conventional Commits with a body explaining why. Every message line
is at most 72 characters.
