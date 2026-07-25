# Goal: Apply Cross-Package Follow-Up Polish

## Mission

Execute additive follow-up work discovered after the original package goals and
hardening goals were completed or started. Do not rewrite those historical goal
files. Coordinate package-specific `GOAL_POLISH.md` prompts, root comparative
benchmarks, specification decisions, and documentation as one auditable pass.

## Required Work

- Execute every package-local `.ai/GOAL_POLISH.md` in dependency-aware order.
- Execute `.ai/GOAL_SPECIFICATION_DECISIONS.md` for specification-backed
  packages.
- Execute `.ai/GOAL_BENCHMARKS.md` with correctness-gated comparisons.
- Execute `.ai/GOAL_DOCUMENTATION.md` after package APIs and statuses are
  verified.
- Keep original `.ai/GOAL.md` and `.ai/GOAL_HARDEN.md` files unchanged.
- Record every user-visible result in the appropriate changelog.
- Re-run package and root release gates after follow-up changes.

## Completion Criteria

- Every package polish prompt is implemented with tests and documentation.
- Root catalogs, recommendations, comparisons, and status reflect reality.
- No follow-up requirement exists only in conversation history.
- Original goal history remains unchanged and auditable.
