# Security policy

## Supported version

Security fixes apply to the current unreleased line until the first v1 release
establishes a maintained version policy.

## Reporting

Report suspected vulnerabilities privately through GitHub security advisories.
Do not place credentials, customer data, production identifiers, or exploit
payloads in public issues, fixtures, events, benchmarks, or mutation reports.

## Model

The core performs no network, filesystem, process, environment, unsafe, or
reflection-driven registration work. It does use reflection narrowly to reject
typed nil interfaces during construction. Identifiers, event history,
resources, work totals, concurrency, rolling windows, and permit lifetime are
bounded.

Custom policies, operations, observers, clocks, and distributed budget
implementations are untrusted caller code. Observer panics are recovered and
callbacks run outside budget locks. Callers remain responsible for operation
idempotency, authorization, secret-safe identifiers, and bounded external IO.
