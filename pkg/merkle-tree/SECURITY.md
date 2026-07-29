# Security

Report suspected vulnerabilities privately to the repository maintainers
before public disclosure.

Callers must treat a root as trusted only when its profile, version, hash
algorithm, tree size, and source are trusted together. Never interpret
successful Merkle verification as proof that application data is true,
authorized, or fresh.

The current pre-v1 surface accepts bounded raw leaves and performs no storage
or network I/O. Configure limits below attacker-controlled resource budgets
and propagate cancellation or deadlines through every operation.
