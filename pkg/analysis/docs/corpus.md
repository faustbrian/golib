# Corpus precision and performance

`make corpus` runs every entry in `corpus/manifest.tsv` as a complete module,
once with normal package parallelism and once with `-sequential`. The reports
must be byte-identical and must match the reviewed JSON baseline. A blocking
finding, analyzer failure, nondeterministic report, missing baseline, malformed
entry, or unexplained report change fails the command.

The tab-separated manifest has four fields:

```text
name<TAB>module<TAB>policy<TAB>baseline
```

Names use lowercase letters, digits, dots, underscores, or hyphens. Paths are
relative and cannot escape their roots. The checked-in manifest exercises the
reusable OSS policy corpus. Organizations keep their owned-repository manifest,
policies, and reviewed baselines in their canonical policy repository rather
than adding private paths here.

Set `CORPUS_MODULE_ROOT` to the directory containing owned repositories and
set both `CORPUS_POLICY_ROOT` and `CORPUS_BASELINE_ROOT` to the canonical
policy checkout. Policy and baseline fields are resolved from those explicit
roots, independent of the invocation directory:

```sh
make build
CORPUS_MODULE_ROOT=/workspace/repos \
CORPUS_POLICY_ROOT=/workspace/policy \
CORPUS_BASELINE_ROOT=/workspace/policy \
  ./scripts/corpus.sh check /workspace/policy/corpus/manifest.tsv
```

When unpublished organization modules depend on other exact local checkouts,
also set `CORPUS_REPLACE_ROOT` to their common absolute parent. The runner
creates one temporary workspace per target module, adds at most 128 direct
child modules as local replacements, and leaves every target `go.mod`
untouched. Duplicate module paths and malformed module directives fail the
run. Without this explicit mode, every entry runs with `GOWORK=off`.

```sh
CORPUS_MODULE_ROOT=/workspace/repos \
CORPUS_POLICY_ROOT=/workspace/policy \
CORPUS_BASELINE_ROOT=/workspace/policy \
CORPUS_REPLACE_ROOT=/workspace/repos \
  ./scripts/corpus.sh check /workspace/policy/corpus/manifest.tsv
```

Use `check` in CI. Use `update` only after classifying every report difference
as a fixed violation, intentional advisory, reviewed suppression, policy
change, or analyzer defect. Baseline changes receive the same review as source
changes; they are not an automatic approval mechanism.

Changed-package execution may be used for fast local feedback, but it does not
replace this full-module corpus gate. Organization release evidence records the
tool checksum, policy revision, repository revisions, report baselines, cold
and warm wall time, and peak memory for the complete corpus. Representative
small, library, and service measurements remain useful for attributing a
full-corpus regression to a repository class.

`make performance` enforces those measurements for the checked-in OSS corpus.
Its six-column tab-separated manifest names the module, policy, maximum cold
and warm milliseconds, and maximum peak resident KiB. The first invocation is
the cold observation and the immediately repeated invocation is the warm
observation; the runner does not purge operating-system or Go caches. It writes
the measured evidence to `.build/performance.tsv` without repository paths or
source. Linux and macOS use `/usr/bin/time`; unsupported hosts fail explicitly.

Organizations should maintain budgets for representative small, library, and
service modules beside their canonical corpus policy and run:

```sh
CORPUS_MODULE_ROOT=/workspace/repos \
CORPUS_POLICY_ROOT=/workspace/policy \
PERFORMANCE_REPORT=/workspace/evidence/performance.tsv \
  ./scripts/performance.sh /workspace/policy/corpus/performance.tsv
```

Budget increases require the same explicit review as changed report baselines.

## Complete owned-repository evidence

`make owned-corpus` discovers every direct `go-*` module beside this checkout,
uses the empty advisory policy unless `OWNED_CORPUS_POLICY` names another exact
policy, and writes private evidence beneath `.build/owned-corpus`. The gate
runs every module in parallel and sequential modes, records reviewed report
baselines, then reruns the complete corpus against those baselines.

Before, between, and after those passes it records each repository HEAD plus
hashes of its tracked diff and untracked paths and contents. Any repository
change during collection invalidates the evidence instead of producing a
mixed-revision baseline. Unpublished sibling modules are resolved through
temporary workspace replacements; target `go.mod` files remain untouched.

Use explicit absolute paths when the repositories, policy, or private evidence
live elsewhere:

```sh
OWNED_CORPUS_ROOT=/workspace/repos \
OWNED_CORPUS_POLICY=/workspace/policy/advisory.yml \
OWNED_CORPUS_EVIDENCE_ROOT=/workspace/evidence/analysis \
  make owned-corpus
```

The evidence directory contains `manifest.tsv`, `revisions.tsv`,
`performance.tsv`, the exact policy, and one deterministic JSON report per
repository. The performance record measures the existing complete update pass
as cold and the complete check pass as warm, records the higher peak resident
memory, and enforces default bounds of 180 seconds per pass and 512 MiB. Set
`OWNED_CORPUS_MAX_COLD_MS`, `OWNED_CORPUS_MAX_WARM_MS`, and
`OWNED_CORPUS_MAX_PEAK_KIB` only through reviewed release policy. The evidence
can contain sensitive repository metadata and MUST NOT be published without
review.

When sibling worktrees are changing, set `OWNED_CORPUS_SOURCE=head`. The runner
archives every exact committed HEAD into a read-only temporary module tree and
records its commit, tree, archive, and extracted-content hashes. The corpus
then proves those listed committed revisions without observing or modifying
concurrent uncommitted work:

```sh
OWNED_CORPUS_SOURCE=head make owned-corpus
```
