# Mutation testing

The reviewed baseline was refreshed on 2026-07-22 with Gremlins v0.6.0:

```sh
GOWORK=off make mutation
```

The calibrated run used four workers, one test CPU per worker, and a timeout
coefficient of 50. Lower coefficients were rejected as invalid evidence after
they classified ordinary passing mutations as timeouts under concurrent load.
The gate independently rejects every `LIVED` or `TIMED OUT` report and
preserves Gremlins' exit status through temporary-file cleanup.

The accepted portable campaign reports:

- 894 killed;
- 0 lived;
- 0 not covered;
- 0 timed out;
- 0 non-viable;
- 100.00% test efficacy; and
- 100.00% mutator coverage.

The portable campaign excludes `scripts/`, whose build-ignored release helper
is executed by the reproducibility gate rather than imported as package code.
It also excludes the operating-system-specific `terminal/echo_*.go` files;
only one implementation can execute on a mutation host, while platform CI
builds and tests every supported target. Portable core and terminal-adapter
production files have no uncovered candidates. Any lived, timed-out, or
portable uncovered mutant remains a release blocker.
