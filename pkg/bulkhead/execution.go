package bulkhead

import (
	"context"
	"errors"
	"reflect"
	"time"
)

var errAdmissionInvariant = errors.New("bulkhead admission invariant violated")

type executionContextKey struct{}

type executionScope struct {
	bulkhead *Bulkhead
	parent   *executionScope
}

func (scope *executionScope) contains(target *Bulkhead) bool {
	for current := scope; current != nil; current = current.parent {
		if current.bulkhead == target {
			return true
		}
	}
	return false
}

// Result reports queue and execution duration separately for an admitted call.
type Result struct {
	Resource          string
	Weight            int64
	WaitDuration      time.Duration
	ExecutionDuration time.Duration
}

// Execute admits operation, invokes it with same-resource reentrancy detection,
// and releases capacity on return or panic. Cancellation cannot terminate an
// operation that ignores its context; such an operation retains capacity until
// it actually returns.
func Execute[T any](
	ctx context.Context,
	bulkhead *Bulkhead,
	weight int64,
	operation func(context.Context) (T, error),
) (value T, result Result, err error) {
	switch reflect.ValueOf(operation).IsNil() {
	case true:
		return value, result, ErrInvalidOperation
	}
	permit, err := bulkhead.Acquire(ctx, weight)
	switch err {
	case nil:
	default:
		return value, result, err
	}
	return executeAdmitted(ctx, bulkhead, weight, permit, operation)
}

func executeAdmitted[T any](
	ctx context.Context,
	bulkhead *Bulkhead,
	weight int64,
	permit *Permit,
	operation func(context.Context) (T, error),
) (value T, result Result, err error) {
	if permit == nil {
		return value, result, errAdmissionInvariant
	}
	result = Result{Resource: bulkhead.config.resource, Weight: weight, WaitDuration: permit.waitDuration}
	executionStart := bulkhead.now()

	defer func() {
		recovered := recover()
		result.ExecutionDuration = nonNegativeDuration(bulkhead.now().Sub(executionStart))
		_ = permit.Release()
		bulkhead.recordExecution(result)
		if recovered != nil {
			panic(recovered)
		}
	}()

	parent, _ := ctx.Value(executionContextKey{}).(*executionScope)
	executionContext := context.WithValue(ctx, executionContextKey{}, &executionScope{
		bulkhead: bulkhead,
		parent:   parent,
	})
	value, err = operation(executionContext)
	return value, result, err
}

func (bulkhead *Bulkhead) recordExecution(result Result) {
	bulkhead.mu.Lock()
	bulkhead.executions++
	bulkhead.totalExecution += result.ExecutionDuration
	event := bulkhead.eventLocked(
		EventExecuted, "", result.Weight, result.WaitDuration, result.ExecutionDuration,
	)
	bulkhead.mu.Unlock()
	bulkhead.observe(event)
}
