# Contributing

Behavior changes start with a failing observable regression. Keep provider
semantics in their adapter instead of expanding the core to a false common
denominator. New parsers require a maintained implementation, corpus seeds,
bounds, fuzzing, and provenance.

Run `make check` during development and `make check-release` before release.
Every Go test, fuzz, race, benchmark, and mutation command in this module uses
`scripts/with-gocache.sh`, which creates and removes a disposable build cache.
