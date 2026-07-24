# Security policy

## Supported versions

Security fixes are developed for the latest released version. Until a stable
release exists, only the current `main` branch is supported.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private
security advisory flow for the repository and include:

- affected version or commit;
- the smallest reproducible input;
- expected and observed impact;
- whether external resolution was enabled and its policy;
- any proposed mitigation.

Do not include production credentials, private documents, fetched schemas, or
customer data. Maintainers will acknowledge a complete report, assess scope,
coordinate a fix, and publish credit according to the reporter's preference.

## Security boundary

Parsing and core validation perform no I/O. External fetching is disabled by
default and requires an explicit resolver store and allowlist. See
[the resolver threat model](docs/security.md) for operational controls.
