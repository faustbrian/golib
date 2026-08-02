# Migration

Begin with `NewFixedAlgorithm` at the existing safe concurrency and immediate
rejection. Verify classification, rejection handling, metrics, and that every
retry or hedge attempt acquires separately. Then run an adaptive algorithm in a
shadow/control comparison before enforcing its decisions.

Choose per-pod bounds from downstream fleet capacity and maximum replicas.
Deploy a conservative initial limit, one workload boundary at a time. Compare
goodput, tail latency, rejection, queueing, and recovery against the fixed
control. Roll back by restoring the fixed profile; no persistent state or data
migration is required.

Do not migrate a reusable fixed semaphore into this module; use `bulkhead`.
Do not replace rate quotas, breakers, timeouts, retries, or HPA with adaptive
concurrency.
