# Contributing

Behavior changes require an observable contract, a focused failing test before
production changes, exact meaningful statement coverage, and exact viable
mutation kills. Concurrent changes must pass race, state-machine, cancellation,
panic, permit-conservation, observer-reentrancy, and cleanup campaigns.

Run `make check` and the repository module contract before delivery. Update
`CHANGELOG.md` and the affected user documentation for every observable change.
Do not add a focused resilience algorithm, hidden preset, global state,
transport dependency, or distributed claim to this module.
