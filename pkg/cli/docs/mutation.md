# Mutation testing

`make mutation` delegates to the canonical repository runner. The gate requires
exact 100% efficacy and mutator coverage: every viable production mutant must
be discovered, executed, and killed.

The test-support package, checked-in reference application, and reference
generator are excluded from mutation scoring. They are development tooling,
remain covered by ordinary tests and generated-file drift checks, and are not
part of the runtime command framework.

Any lived, uncovered, timed-out, malformed, missing, or unclassified result is
a release blocker. Historical standalone reports with uncovered mutants are
retained only as audit history and do not satisfy the current gate.
Successful release evidence therefore contains no survivors, timeouts, or
uncovered viable mutants.
