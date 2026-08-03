# Mutation testing

Run the canonical content-addressed campaign with:

```sh
GOWORK=off make mutation
```

The package command delegates to the same runner used by CI. It discovers
production packages from the repository manifest, uses the pinned toolchain,
and requires exact 100.00% efficacy and mutant coverage. Any lived, timed-out,
uncovered, malformed, missing, or unclassified mutant remains a release
blocker; package-local exclusions and threshold overrides are forbidden.
