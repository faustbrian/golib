package validation_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	validation "github.com/faustbrian/golib/pkg/validation"
)

type panickingAsyncValidator struct{}

func (panickingAsyncValidator) ValidateAsync(context.Context,
	validation.Context, int,
) validation.Report {
	panic("token=secret")
}

func TestAsyncAllBoundsConcurrencyAndPreservesDeclarationOrder(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxCustomConcurrency = 2
	vctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	validators := make([]validation.AsyncValidator[string], 3)
	for index := range validators {
		index := index
		validators[index] = validation.AsyncValidatorFunc[string](func(
			ctx context.Context, vctx validation.Context, _ string,
		) validation.Report {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			return validation.NewReport(vctx.Limits()).Add(validation.NewViolation(
				vctx.Path(), []string{"a", "b", "c"}[index], validation.Error, nil, nil,
			))
		})
	}
	var report validation.Report
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		report = validation.AsyncAll(context.Background(), vctx, "value", validators...)
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("validators did not start")
		}
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d", maximum.Load())
	}
	close(release)
	wait.Wait()
	violations := report.Violations()
	if len(violations) != 3 || violations[0].Code() != "a" ||
		violations[1].Code() != "b" || violations[2].Code() != "c" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestAsyncAllStopsSchedulingAfterCancellation(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxCustomConcurrency = 1
	vctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := validation.AsyncAll[int](context.Background(), vctx, 1); !report.Empty() {
		t.Fatalf("empty validators = %v", report)
	}
	if report := validation.AsyncAll[int](context.Background(), vctx, 1, nil); !report.Empty() {
		t.Fatalf("nil validator = %v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	first := validation.AsyncValidatorFunc[int](func(
		context.Context, validation.Context, int,
	) validation.Report {
		close(started)
		<-release
		return validation.NewReport(vctx.Limits())
	})
	var secondCalls atomic.Int32
	second := validation.AsyncValidatorFunc[int](func(
		context.Context, validation.Context, int,
	) validation.Report {
		secondCalls.Add(1)
		return validation.NewReport(vctx.Limits())
	})
	done := make(chan validation.Report)
	go func() { done <- validation.AsyncAll(ctx, vctx, 1, first, second) }()
	<-started
	cancel()
	time.AfterFunc(10*time.Millisecond, func() { close(release) })
	report := <-done
	if !report.Empty() || secondCalls.Load() != 0 {
		t.Fatalf("report=%v second calls=%d", report, secondCalls.Load())
	}
}

func TestAsyncAllContainsArbitraryPanicsAndHonorsDeadlines(t *testing.T) {
	vctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := validation.AsyncAll(context.Background(), vctx, 1,
		panickingAsyncValidator{})
	if !report.HasCode("validator_panic") || !report.HasErrors() {
		t.Fatalf("panic report = %#v", report.Violations())
	}
	report = validation.IsolateAsyncPanics[int](panickingAsyncValidator{}).
		ValidateAsync(context.Background(), vctx, 1)
	if !report.HasCode("validator_panic") || !report.HasErrors() {
		t.Fatalf("isolated panic report = %#v", report.Violations())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	deadlineAware := validation.AsyncValidatorFunc[int](func(ctx context.Context,
		vctx validation.Context, _ int,
	) validation.Report {
		<-ctx.Done()
		return validation.NewReport(vctx.Limits()).Add(validation.NewViolation(
			vctx.Path(), "deadline", validation.Warning, nil, nil,
		))
	})
	done := make(chan validation.Report, 1)
	go func() {
		done <- validation.AsyncAll(ctx, vctx, 1, deadlineAware)
	}()
	select {
	case report = <-done:
		if !report.HasCode("deadline") || report.HasErrors() {
			t.Fatalf("deadline report = %#v", report.Violations())
		}
	case <-time.After(time.Second):
		t.Fatal("deadline-aware validation did not terminate")
	}
}
