# Contributing

Use a focused branch and conventional commits. Every behavior change starts
with a failing regression, vector, or conformance test. Run `make check` before
opening a pull request. Never add real webhook payloads, signatures, endpoint
tokens, event IDs, or secrets to tests, fixtures, issues, or logs.

Public API, canonical bytes, emitted headers, envelope encoding, error
identity, retry classification, and provider presets are compatibility
surfaces. Describe any change to them under `Unreleased` in `CHANGELOG.md`.

Provider presets require an authoritative specification link, independent
positive and negative fixtures, and a named maintenance owner. A copied blog
post or SDK snippet is insufficient evidence.
