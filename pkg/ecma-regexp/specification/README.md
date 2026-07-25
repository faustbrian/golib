# Specification provenance

The implementation target is exactly ECMA-262, 16th edition, also known as
ECMAScript 2025. `manifest.json` records the upstream tag, commit, published
PDF, and digest. Future editions are unsupported until their grammar and
semantic differences have their own inventory and executable evidence.

Test262 is pinned by commit. The RegExp-literal negative parse slice accounts
for all 186 tests: 167 execute against the package and 19 are explicitly
excluded because they test JavaScript source tokenization rather than Pattern
grammar. All 52 other literal files provide positive syntax evidence; 12 make
matcher calls and 40 only exercise syntax or JavaScript evaluation behavior.

The pinned `built-ins/RegExp` inventory contains 1,868 files. The harness
rejects all 192 negative pattern literals, compares 443 generated class and
Unicode property fixtures directly with the engine tables, and delegates the
matcher calls from 494 selected feature files to the Go engine. Sixty selected
files make no matcher call. The remaining 679 files specify JavaScript
constructor, prototype, species, or `RegExp.escape` object behavior that this
Go package does not expose. Exact accounting is in `conformance/test262.tsv`.

`conformance/requirements.tsv` maps normative obligations to executable
evidence. A row is complete only when its evidence exercises the whole stated
requirement. `conformance/decisions.tsv` records deliberate interpretations,
ambiguities, and errata decisions.

`conformance/features.tsv` reconciles every Test262 feature tag present in the
pinned RegExp corpus and records the only current RegExp proposal outside the
2025 snapshot. `conformance/errata.tsv` records the publication-index and live
draft audit boundary. Feature-tag counts are enforced by the Test262 gate.

`conformance/differential.tsv` records the pinned CI runtimes, their underlying
engine families, vector counts, and every tolerated engine divergence. The
release gate requires at least two installed JavaScript runtimes and includes
both V8 and JavaScriptCore in CI. A divergence is never silently skipped.

Unicode code-point properties are generated from the pinned Unicode 16.0.0
UCD archive. Run `go generate` to download, digest-check, and reproduce the
tables. Unicode Sets properties of strings are tracked separately and are not
claimed by the code-point table generator.
