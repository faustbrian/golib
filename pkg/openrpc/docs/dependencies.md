# Dependencies

The runtime dependency surface is deliberately small:

- `regexp2` supplies bounded ECMAScript-compatible Draft 7 regular expressions.
- `jsonschema/v6` supplies Draft 7 compilation and validation behind
  package-owned `jsonschema` contracts with no URL loader.
- `x/text` is currently indirect through the validation dependency.

Versions are pinned in `go.mod` and checksummed in `go.sum`. Dependency updates
must run the full Draft 7, OpenRPC meta-schema, conformance, fuzz, race,
benchmark, license, and vulnerability gates. Replacement must not change exact
numbers, regex semantics, reference loading, diagnostic safety, or boolean
schema behavior.

Pinned OpenRPC inputs are not Go dependencies. Their repository commits,
normalized hashes, and license copies are in `specification/manifest.json` and
verified offline by tests.
