# Contributing

Use Go 1.26.5 and make focused changes. New behavior starts with a failing test
that proves the missing contract. Security decisions need boundary, malformed,
cancellation, panic, and ordering cases.

Run `make check` before submitting a change. Public APIs require documentation,
an API baseline update, changelog entry, compatibility analysis, and tests.
Never weaken the 100% production statement coverage gate with generated or
test-only files. Do not add globals, hidden registration, unsafe, cgo,
reflection discovery, network calls, or background goroutines.

Commits use Conventional Commits with a body explaining why. Every message line
is at most 72 characters.
