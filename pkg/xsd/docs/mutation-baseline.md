# Mutation baseline

`make mutation` delegates the complete production module to the canonical
content-addressed repository runner. The pinned runner requires exact 100.00%
test efficacy and mutant coverage. Every viable mutant must be killed; a
survivor, timeout, uncovered mutant, malformed report, missing package, or
unclassified result fails the gate. Package-local thresholds, exclusions, and
stored result files are not release evidence.
