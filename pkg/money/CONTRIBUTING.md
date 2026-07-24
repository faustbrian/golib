# Contributing

Use Go 1.26.5 or newer and work from the repository's required branch workflow.
Behavior changes begin with a failing focused test and preserve the exact
currency, context, and conservation contracts.

Run:

```sh
make check
make release-check
```

New public APIs require documentation, examples, compatibility-baseline review,
and tests for zero values, bounds, aliases, cancellation, serialization, and
currency/context mismatches. New rounding or allocation policy requires a
conservation property and a killed mutation. Do not introduce float-based
monetary APIs, a second decimal implementation, live FX access, unsafe code, or
customer data in fixtures.
