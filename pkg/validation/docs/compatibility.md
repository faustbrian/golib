# Compatibility

The minimum supported toolchain is Go 1.26.5. The module has no runtime
third-party dependencies. Linux, macOS, and Windows are supported where the Go
standard library is supported; CI's primary environment is Linux.

The v1 compatibility contract includes exported Go API, sentinel relationships,
stable standard-rule codes, path rendering and JSON-pointer escaping,
declaration-order aggregation, deduplication identity, and transport field
names. Application translations, custom codes, benchmark timings, and
observation backend adapters are outside that contract.

`api/baseline.txt` is the mechanical exported-API snapshot. It complements
behavior tests; it does not prove semantic compatibility by itself.
