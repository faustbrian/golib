# Roadmap

## v1 readiness completed

- Backend integration fixtures are hermetic in-process tests or repeatable
  tagged containers.
- Meaningful 100% production-code coverage is enforced in CI.
- Redis enqueue, consume, ack, retry, and shutdown benchmarks run in CI.
- Lifecycle events carry backend/queue identity; Redis Streams exposes group
  depth and oldest-job age, while other backend-specific sources are documented.
- Examples and documentation links are validated in CI.
- Dependency updates, vulnerability scanning, and tagged releases are automated.

## After v1

- Optional middleware/interceptor chain.
- Deduplication and delayed-delivery helpers.
- Maintained metrics exporters outside the core contract.

Dead-letter settlement and Redis pending recovery are v1 requirements tracked
by `.ai/GOAL_DEAD_LETTER.md`, not post-v1 helpers.
