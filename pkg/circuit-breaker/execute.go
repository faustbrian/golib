package breaker

import (
	"context"
	"time"
)

// Execute admits and invokes operation, classifies its completion, and returns
// the original typed result and operation error. A rejected operation is never
// invoked. Panics are recorded as failures and re-panicked with the same value.
func Execute[T any](
	ctx context.Context,
	b *Breaker,
	operation func(context.Context) (T, error),
) (result T, operationErr error) {
	permit, err := b.acquire(ctx, true)
	if err != nil {
		return result, err
	}
	started, clockPanic := safeClockNow(b.config.clock)
	if clockPanic != nil {
		_ = permit.cancelAt(permit.admissionTime())
		panic(clockPanic)
	}

	defer func() {
		if panicValue := recover(); panicValue != nil {
			finished, finishPanic := safeClockNow(b.config.clock)
			if finishPanic != nil {
				finished = started
			}
			duration := elapsed(finished, started)
			_ = permit.completeAt(
				OutcomeFailure,
				duration >= b.config.slowCallDuration,
				finished,
				b.jitterSample(),
			)
			panic(panicValue)
		}
	}()

	result, operationErr = operation(ctx)
	finished, clockPanic := safeClockNow(b.config.clock)
	if clockPanic != nil {
		panic(clockPanic)
	}
	duration := elapsed(finished, started)
	outcome := b.config.classifier(Completion{
		Context:  ctx,
		Result:   result,
		Err:      operationErr,
		Duration: duration,
	})
	completionErr := permit.completeAt(
		outcome,
		duration >= b.config.slowCallDuration,
		finished,
		b.jitterSample(),
	)
	if completionErr != nil {
		_ = permit.cancelAt(finished)
	}
	if operationErr != nil {
		return result, operationErr
	}
	return result, completionErr
}

func safeClockNow(clock Clock) (now time.Time, panicValue any) {
	defer func() { panicValue = recover() }()
	now = clock.Now()
	return now, nil
}

func elapsed(finished, started time.Time) time.Duration {
	duration := finished.Sub(started)
	if duration < 0 {
		return 0
	}
	return duration
}
