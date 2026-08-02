package concurrencylimit

import "context"

// Execute acquires a permit, invokes operation once, classifies its completion,
// and records exactly one terminal outcome. A rejected operation is not called.
func Execute[T any](
	ctx context.Context,
	limiter *Limiter,
	operation func(context.Context) (T, error),
) (T, error) {
	return ExecuteWithMetadata(ctx, limiter, Metadata{}, operation)
}

// ExecuteWithMetadata is Execute with bounded diagnostic admission metadata.
func ExecuteWithMetadata[T any](
	ctx context.Context,
	limiter *Limiter,
	metadata Metadata,
	operation func(context.Context) (T, error),
) (result T, resultErr error) {
	permit, err := limiter.Acquire(ctx, metadata)
	if err != nil {
		return result, err
	}
	if operation == nil {
		_ = permit.Complete(OutcomeLocalDrop)
		return result, ErrInvalidOutcome
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			_ = permit.Complete(OutcomeDependencyFailure)
			panic(panicValue)
		}
	}()

	result, resultErr = operation(ctx)
	finished, ok := safeNow(limiter.config.clock)
	if !ok {
		limiter.incrementClockError()
		_ = permit.Complete(OutcomeIgnored)
		return result, ErrClock
	}
	duration := max(finished.Sub(permit.started), 0)
	completion := Completion{Context: ctx, Result: result, Err: resultErr, Duration: duration}
	outcome, classifierOK := safeClassify(limiter.config.classifier, completion)
	if !classifierOK {
		limiter.incrementClassifierPanic()
		_ = permit.Complete(OutcomeIgnored)
		return result, ErrClassifierPanic
	}
	if !outcome.valid() {
		_ = permit.Complete(OutcomeIgnored)
		return result, ErrInvalidOutcome
	}
	completionErr := permit.Complete(outcome)
	if resultErr != nil {
		return result, resultErr
	}
	return result, completionErr
}

func defaultClassifier(completion Completion) Outcome {
	if completion.Context != nil && completion.Context.Err() != nil {
		return OutcomeIgnored
	}
	if completion.Err != nil {
		return OutcomeDependencyFailure
	}
	return OutcomeSuccess
}

func safeClassify(classifier Classifier, completion Completion) (outcome Outcome, ok bool) {
	defer func() { ok = recover() == nil }()
	return classifier(completion), true
}
