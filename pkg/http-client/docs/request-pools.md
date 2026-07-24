# Bounded Request Pools

Request pools provide bounded in-process fan-out over caller-owned HTTP
operations. They do not replace `http.Transport` connection pooling and do not
construct vendor requests or decode vendor responses.

```go
pool, err := httpclient.NewPool(
	httpclient.PoolOptions[WidgetID, Widget]{
		Concurrency: 4,
		Pending:     8,
		Order:       httpclient.PoolInputOrder,
		Failure:     httpclient.PoolCollectAll,
		Limits: httpclient.PoolLimits{
			MaximumRequests:      1_000,
			MaximumElapsed:       30 * time.Second,
			MaximumResponseBytes: 32 << 20,
			MaximumMemoryBytes:   64 << 20,
		},
		Key: func(id WidgetID) (string, error) {
			return id.String(), nil
		},
		Execute: func(
			ctx context.Context,
			id WidgetID,
		) (httpclient.PoolValue[Widget], error) {
			widget, responseBytes, err := getWidget(ctx, id)

			return httpclient.PoolValue[Widget]{
				Value:         widget,
				ResponseBytes: responseBytes,
				MemoryBytes:   widget.MemoryBytes(),
			}, err
		},
	},
)
results, runErr := pool.RunSlice(ctx, widgetIDs)
```

`Execute` owns the typed vendor operation. Calling `Client.Do` from that
callback creates one logical operation per input, so operation identity and a
generated idempotency key remain stable across that input's retries while
remaining distinct from every other pool item.

## Sources and backpressure

`RunSlice` snapshots the input slice before starting work. `RunGenerator`
pulls one item at a time from a lazy callback. `RunChannel` reads until channel
closure. A generator must observe its context and return promptly after
cancellation; a channel source is canceled by the pool without requiring the
producer to close the channel.

The pool creates a fixed number of workers for each run. `Pending` bounds the
job channel between the single source scheduler and those workers. It never
creates one goroutine per input. A slow transport, limiter, retry delay, or
circuit admission therefore backpressures the source instead of producing
unbounded pending work.

Zero configuration resolves to four workers, pending capacity equal to the
worker count, a minimum concurrency of one, and a maximum concurrency of 32.
`SelectConcurrency` may choose a run-wide worker count from `PoolWorkload` but
must remain inside the configured minimum and maximum. A known slice reports
its exact request count; generator and channel sources report `-1`.

## Keys and ordering

Each result contains its source index, input, key, typed value, declared byte
accounting, and request error. With no `Key` callback, the decimal source index
is used. Custom keys may correlate results with caller state, but should remain
bounded and must not be used as uncontrolled telemetry labels. Pool errors do
not render keys, inputs, callback errors, or panic values.

`PoolInputOrder` is the default and sorts completed results by stable source
index. `PoolCompletionOrder` preserves the order in which worker results reach
the pool. Execution remains concurrent in either mode; ordering only affects
the returned slice.

## Partial failures and cancellation

`PoolCollectAll` is the default. An executor failure remains on its individual
`PoolResult.Error`, other requests continue, and the overall run error remains
nil. Callers must inspect every result.

`PoolFailFast` retains the failing result, cancels pending and in-flight work,
and returns a `PoolError` that unwraps the first request failure. Already
completed results remain available. Concurrent callbacks that observe
cancellation may also return cancellation results before the run finishes.

Source, key, configuration, budget, and cancellation failures terminate the
run and return completed partial results with a `PoolError`. Selector, source,
key, and executor panics are contained as `PoolPanicError`; panic values are
available programmatically but never rendered. Executors and generators must
honor context cancellation. `RunSlice`, `RunGenerator`, and `RunChannel` do not
return until their workers and elapsed-budget waiter have stopped.

## Finite budgets

Zero values resolve to finite run-wide defaults:

- 10,000 requests;
- five minutes elapsed time;
- 64 MiB of declared response bytes; and
- 64 MiB of declared retained memory.

`RunSlice` rejects a known over-budget input before starting any worker.
Generator and channel sources stop pulling before requesting an item beyond
the request maximum. Because an unknown-length source cannot look ahead safely,
reaching its maximum returns `ErrPoolLimit` even when the next source read would
have reported completion.

The elapsed budget owns a context-aware waiter and cancels the run at expiry.
The executor must report non-negative `ResponseBytes` and `MemoryBytes` for
every result. A result that would overflow an integer or exceed a byte or
memory budget is not retained. The pool does not estimate object graphs or
silently buffer response bodies; accounting remains explicit at the typed
vendor boundary.

## Policy composition

Use one shared hardened `Client` from every executor. The execution pool limits
logical-operation concurrency, while `http.Transport` independently limits
connections. Retry middleware remains inside one executor call, so retries do
not create new pool workers. Attempt-level rate limiting still admits every
physical retry or redirect, and the circuit breaker still observes one logical
operation outcome.

Do not place another unbounded fan-out layer inside `Execute`. Nested pools can
multiply concurrency and should instead share an explicit outer budget.

Cursor and Link pagination remain sequential. Only submit pagination work to a
pool when page numbers, offsets, or independent continuations are known to be
safe concurrently. Preserve page keys and use input-order results before
flattening pages when vendor ordering matters.

The pool does not own response bodies. `Execute` must apply the same response
read, decode, drain, and close contract as a direct vendor operation before it
returns its typed value or error.
