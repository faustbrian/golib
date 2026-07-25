# Mutation evidence

The 2026-07-22 run used Go 1.26.5 on Darwin/arm64 at package commit
`dc23e7c2f95d425dff35f0f56869f2c6a10b75d2` with Gremlins v0.6.0.

The root command executed 1,016 mutation records: 1,004 were killed by a failing
test, none lived, and 12 could not be mapped to executable statement coverage.
The nested `gomoney` module executed and killed all 25 mutations. The nested
comparison adapter executed 10 mutations: nine were killed and one could not
be mapped to the separately tested `main` process boundary. All three modules
therefore have 100% test efficacy over executed mutants; `gomoney` has 100%
mutator coverage.

The 13 root and adapter exclusions are individually recorded in
`specification/mutation-classifications.tsv`. They are limited to constant
initializers, switch expressions, and a subprocess entry point for which
Gremlins v0.6.0 does not map the expression position to the statement coverage
that the named tests execute. No survivor is waived.

`make mutation` reruns all three modules, rejects every live mutant, verifies the
exact classified set, and compares the fresh machine-readable result with the
tracked raw evidence while ignoring elapsed time. Set
`UPDATE_MUTATION_EVIDENCE=1` only when intentionally refreshing the reviewed
raw artifacts.

Raw files:

- `raw/root.json` SHA-256
  `cfcc406fe7fe6218e74cd8654a180b8f05c9230d3a0be7f04b6f79ac92defe27`
- `raw/gomoney.json` SHA-256
  `acf53dcf792c52afca0f8af30d63ee4570ebaf836d3d11e53020e7a228d59479`
- `raw/adapter.json` SHA-256
  `70d430a64c2d359696d3e22240bc7b29ac2dcaea3b46198209527ce96cc1ed6a`
- `specification/mutation-classifications.tsv` SHA-256
  `4baf3f69ee8d059dbccbcb38c0e00209aa93a26edb520b659da755a079b36a63`
