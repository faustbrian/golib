# Contributing

Behavior changes require an observable contract, a failing focused test before
production changes, meaningful exact statement coverage, and mutation proof.
Concurrent changes must also pass the race and stress gates and preserve permit
conservation, FIFO order, cancellation cleanup, shutdown, and observer
lock-exclusion.

Run `make check` for the local contract and the repository affected-module gate
before delivery. Update `CHANGELOG.md` and public documentation for every
user-visible change. Do not add priority, resizing, leases, resource identity,
distributed coordination, or adaptive policy without a separate normative
goal.
