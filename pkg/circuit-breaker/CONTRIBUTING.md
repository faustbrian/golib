# Contributing

Use a conventional or Linear-style branch from `main`, add a changelog entry
for every implementation change, and preserve the protocol-neutral boundary.
Behavior changes require a failing regression or model counterexample first.

Before opening a pull request:

```sh
make tools
make check
actionlint .github/workflows/*.yml
```

Pull requests must explain the dependency failure mode, caller/core ownership,
state or compatibility impact, and verification evidence. New settings must be
finite, validated, deterministic under an injected clock/random source, and
covered at boundaries. Do not introduce retries, timeouts, fallbacks, a global
registry, distributed state, protocol policy, unbounded storage, `unsafe`, cgo,
or hidden runtime hooks into core.

By participating, contributors agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
