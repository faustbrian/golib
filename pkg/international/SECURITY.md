# Security policy

Report vulnerabilities privately through GitHub security advisories. Do not
include real phone numbers, postal codes, credentials, or customer data.

Parsers bound bytes, locale segments, Unicode normalization, extensions,
metadata, and diagnostics. Contracts reject invalid UTF-8 rather than repairing
it. Generated inputs are HTTPS-fetched, size-limited, checksum-pinned,
deterministically transformed, license-reviewed, and drift-checked.

Threats considered include Unicode confusables and normalization mismatch,
numeric ambiguity, regex denial of service, metadata poisoning, dependency
compromise, and sensitive-value leakage. Identifiers are not identity proof;
phone validity is not ownership, and postal syntax is not deliverability.

Exact byte, segment, diagnostic, dataset, and generator budgets and their
hostile-input evidence are maintained in `docs/verification.md`. Unsupported
display locales return no metadata rather than panicking or inferring a locale.

Applications should redact phone and postal values from logs and telemetry,
limit request bodies before decoding, use explicit locale policies, review data
diffs, run vulnerability scanning, and promptly update pinned metadata after an
upstream security advisory.
