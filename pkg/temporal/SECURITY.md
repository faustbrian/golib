# Security policy

## Reporting

Report vulnerabilities privately through GitHub Security Advisories. Do not
include secrets, production data, or exploit payloads in a public issue.

## Supported versions

Before v1, only the latest commit is supported. After v1, the latest minor of
the current major receives security fixes.

## Threat model

The package treats notation, JSON, SQL range text, sequence inputs, and split
parameters as untrusted. `temporal.Limits` bounds parse bytes, precision, error
and output bytes, parser depth, input/output period counts, and steps. Parsers
require valid UTF-8, ASCII grammar tokens, complete consumption, unique ordered
components, and checked arithmetic.

Set operations reject cardinality expansion before returning partial output.
Iterators reject zero and negative steps and prove progress. Returned slices
are copies. PostgreSQL adapters reject unbounded, empty, NULL-as-value, and
microsecond-loss cases unless represented explicitly by nullable wrappers.

DST resolution is outside core algebra. Callers must supply a location and a
`calendar/timezone.Resolution`; no implicit local timezone is consulted.

Known residual risks are documented in [docs/security.md](docs/security.md).
