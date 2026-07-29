# Security

Report suspected vulnerabilities privately to the repository maintainers
before public disclosure.

Callers must treat a root as trusted only when its profile, version, hash
algorithm, tree size, and source are trusted together. Never interpret
successful Merkle verification as proof that application data is true,
authorized, or fresh.

The current pre-v1 surface accepts bounded raw leaves, inclusion proofs, and
append-only consistency proofs and performs no storage or network I/O.
Configure construction and proof limits below attacker-controlled resource
budgets and propagate cancellation or deadlines through every operation. Treat
malformed-proof errors differently from authentication failures only for
diagnostics; neither is permission to accept a leaf or tree history.
