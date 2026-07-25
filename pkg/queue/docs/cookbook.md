# Scenario cookbook

## Fixed-delay retries

Set `RetryCount` and `RetryDelay` on `job.AllowOption`. The total timeout covers
handler execution and retry waits.

## Exponential backoff

Leave `RetryDelay` at zero and set `RetryMin`, `RetryMax`, `RetryFactor`, and
optionally `Jitter`. Jitter is recommended for shared downstream outages.

## Graceful service shutdown

Stop accepting new application requests, call `Queue.Release`, and bound the
outer service shutdown deadline above the longest job timeout. A handler must
honor its context for prompt cancellation.

## Poison messages

Set finite package and backend retry counts and observe `handler_failed` plus
`rejected`. v1 does not hide backend dead-letter differences: configure the
RabbitMQ `DeadLetterConfig`, NSQ `WithDeadLetter`, or Redis Streams record
operations explicitly.

Valkey Streams provides a bounded built-in terminal policy through
`WithDeadLetter(stream, attempts)`. Failed work remains pending until reclaim;
the terminal attempt appends to the dead-letter stream before acknowledging
the source. Deduplicate dead letters by `original_id` because an ambiguous
source acknowledgement can repeat the append.

## Valkey crash recovery

Use a unique consumer name per process and configure `WithReclaim` above the
longest valid handler runtime. A replacement process in the same group claims
idle pending work with `XAUTOCLAIM`. Never use a short reclaim threshold as a
substitute for handler cancellation; it can create concurrent duplicates.

## Duplicate protection

Include an application job identifier in the payload and enforce idempotency at
the side-effect boundary. Broker acknowledgement is not a transaction with your
database or external API.
