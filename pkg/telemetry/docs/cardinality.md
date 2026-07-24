# Cardinality

Metric cardinality is the product of every distinct attribute value
combination. One unbounded identifier can exhaust memory in the service,
Collector, and backend.

## Budgets

The runtime sets a hard per-instrument `CardinalityLimit`, defaulting to 1,000.
Choose a lower value for instruments with a deliberately small contract. The
limit includes overflow handling, so dashboards must not assume every observed
dimension becomes a separate series.

Define attribute allow-lists with metric views. Recommended HTTP dimensions are
normalized method, route template, and response status. Recommended cache and
queue dimensions are the finite operation, outcome, and backend enums supplied
by the adapters.

## Forbidden defaults

Do not use these as metric attributes:

- trace, span, request, user, account, order, job, or cache-key identifiers
- raw URL paths, hosts, queries, SQL, error strings, or queue names from input
- email addresses, tokens, payload data, or arbitrary baggage
- timestamps, random values, hashes of identifiers, or process instance IDs

Hashing an identifier hides its text but preserves its cardinality and is not a
cardinality control.

## Regression testing

Record more unique combinations than the configured limit into a manual reader
and assert the resulting point count. Also assert every point's attribute set.
The `metric` package does this with ten unique inputs and a limit of three.

Monitor active series, SDK overflow, Collector refusal, backend limits, and
process memory. Treat an unexpected increase as a release blocker even when the
hard cap prevents a crash.
