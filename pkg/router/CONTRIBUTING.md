# Contributing

Use Go 1.26.5 or newer. Create a conventional branch from `main`, keep commits
focused, and include tests and documentation for public behavior.

Run `make check-all` before opening a pull request. Changes to matching must add
standard-library differential evidence. Security-sensitive changes must add an
adversarial regression or fuzz seed. Public API changes must update the API
baseline, changelog, and compatibility documentation.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
