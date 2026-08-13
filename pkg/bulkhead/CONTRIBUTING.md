# Contributing

Use Go 1.26.6 or newer within the Go 1 compatibility promise. Preserve strict
FIFO admission, exact permit conservation, bounded identity and queues,
callback-outside-lock behavior, and explicit process-local semantics.

Add a failing behavioral test before a runtime change. Run the repository
module contract with `make check MODULES=pkg/bulkhead`. Public API changes
require an updated compatibility baseline and `CHANGELOG.md` entry.
