# Contributing

Use Go 1.26 or newer. Keep identifiers distinct, transformations explicit, and
core behavior offline. Do not add country business rules, deliverability
claims, identity inference, or sensitive telemetry.

Run `make release-check` before submitting a release candidate and
`make nilaway` for the advisory nilness report. Dataset updates must include
source version, retrieval date, license, checksum, deterministic output,
classified diff, independent vectors, and changelog compatibility notes. Add
tests before changing acceptance or canonicalization behavior.
