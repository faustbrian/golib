# FAQ

## Is relevance portable?

No. Core queries are portable intent; analyzer and ranking behavior belongs to
the adapter, index definition, and application acceptance corpus.

## Is an accepted write immediately searchable?

Not necessarily. Visibility follows refresh policy. Document the application
read-your-write strategy.

## Can I retry a failed bulk request?

Retry individually classified, idempotent items within a bounded budget.
Reconcile unknown outcomes first.

## Why did my cursor stop working?

It may be expired, tampered with, bound to another query or tenant, exceed its
budget, reference an expired PIT, or target a changed mapping fingerprint.

## Does the fake match OpenSearch ranking or analyzers?

No. It is deterministic and bounded for application contract tests only. Run
adapter conformance and real OpenSearch integration tests for backend behavior.
