package prompts_test

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestProgressCoalescesConcurrentUpdatesAndRendersStableLines(t *testing.T) {
	t.Parallel()

	progress, err := prompts.NewProgress(prompts.ProgressConfig{ID: "upload", Label: "Upload", Total: 100})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	var group sync.WaitGroup
	for value := int64(1); value <= 100; value++ {
		group.Go(func() { _ = progress.Update(value, "uploading") })
	}
	group.Wait()
	snapshot := progress.Snapshot()
	if snapshot.Current < 1 || snapshot.Current > 100 || snapshot.Total != 100 || snapshot.State != prompts.ProgressRunning {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	if err := progress.Update(100, "uploaded"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	progress.Complete("done")
	terminal := prompts.NewVirtualTerminal(40, 8)
	if err := progress.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	output := terminal.Output()
	if !strings.Contains(output, "success: Upload: 100/100 (100%) - done") || strings.Contains(output, "\x1b[") {
		t.Fatalf("progress output = %q", output)
	}
}

func TestProgressRejectsRegressionOverflowAndTerminalMutation(t *testing.T) {
	t.Parallel()

	progress, err := prompts.NewProgress(prompts.ProgressConfig{ID: "work", Label: "Work", Total: 10})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	if err := progress.Update(5, "half"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := progress.Update(4, "back"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("regression error = %v", err)
	}
	if err := progress.Update(11, "too far"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("total overflow error = %v", err)
	}
	bounded, err := prompts.NewProgress(prompts.ProgressConfig{ID: "bound", Label: "Bound", Total: 10})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	_ = bounded.Update(5, "half")
	if err := bounded.Increment(6, "too far"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("bounded increment error = %v", err)
	}
	progress.Fail("failed")
	if err := progress.Increment(1, "late"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("terminal update error = %v", err)
	}
	progress.Complete("ignored")
	if snapshot := progress.Snapshot(); snapshot.State != prompts.ProgressFailed || snapshot.Message != "failed" {
		t.Fatalf("terminal Snapshot() = %#v", snapshot)
	}

	indeterminate, err := prompts.NewProgress(prompts.ProgressConfig{ID: "wait", Label: "Wait"})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	if err := indeterminate.Increment(math.MaxInt64, "max"); err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	if err := indeterminate.Increment(1, "overflow"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("increment overflow error = %v", err)
	}
}

func TestCallerDrivenMutationsHonorContext(t *testing.T) {
	t.Parallel()

	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "copy", Label: "Copy", Total: 3,
	})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := progress.UpdateContext(canceled, 1, "one"); !errors.Is(err, prompts.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled UpdateContext() error = %v", err)
	}
	if err := progress.IncrementContext(canceled, 1, "one"); !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("canceled IncrementContext() error = %v", err)
	}
	if snapshot := progress.Snapshot(); snapshot.State != prompts.ProgressPending {
		t.Fatalf("canceled progress mutated state: %#v", snapshot)
	}
	//lint:ignore SA1012 Nil context behavior is part of the public contract.
	if err := progress.UpdateContext(nil, 1, "one"); !errors.Is(err, prompts.ErrInvalidDefinition) { //nolint:staticcheck // contract test
		t.Fatalf("nil-context UpdateContext() error = %v", err)
	}
	deadline, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	if err := progress.UpdateContext(deadline, 1, "one"); !errors.Is(err, prompts.ErrDeadlineExceeded) {
		t.Fatalf("deadline UpdateContext() error = %v", err)
	}
	if err := progress.UpdateContext(context.Background(), 1, "one"); err != nil {
		t.Fatalf("UpdateContext() error = %v", err)
	}
	if err := progress.IncrementContext(context.Background(), 1, "two"); err != nil {
		t.Fatalf("IncrementContext() error = %v", err)
	}

	spinner, err := prompts.NewSpinner(prompts.SpinnerConfig{ID: "wait", Label: "Wait"})
	if err != nil {
		t.Fatalf("NewSpinner() error = %v", err)
	}
	if err := spinner.AdvanceContext(canceled, "blocked"); !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("canceled AdvanceContext() error = %v", err)
	}
	//lint:ignore SA1012 Nil context behavior is part of the public contract.
	if err := spinner.AdvanceContext(nil, "blocked"); !errors.Is(err, prompts.ErrInvalidDefinition) { //nolint:staticcheck // contract test
		t.Fatalf("nil-context AdvanceContext() error = %v", err)
	}
	if err := spinner.AdvanceContext(context.Background(), "ready"); err != nil {
		t.Fatalf("AdvanceContext() error = %v", err)
	}
	if snapshot := spinner.Snapshot(); snapshot.Message != "ready" {
		t.Fatalf("spinner Snapshot() = %#v", snapshot)
	}

	stream, err := prompts.NewStatusStream(1)
	if err != nil {
		t.Fatalf("NewStatusStream() error = %v", err)
	}
	if err := stream.AppendContext(canceled, prompts.StatusInfo, "blocked"); !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("canceled AppendContext() error = %v", err)
	}
	//lint:ignore SA1012 Nil context behavior is part of the public contract.
	if err := stream.AppendContext(nil, prompts.StatusInfo, "blocked"); !errors.Is(err, prompts.ErrInvalidDefinition) { //nolint:staticcheck // contract test
		t.Fatalf("nil-context AppendContext() error = %v", err)
	}
	if err := stream.AppendContext(context.Background(), prompts.StatusSuccess, "done"); err != nil {
		t.Fatalf("AppendContext() error = %v", err)
	}
	if entries := stream.Snapshot(); len(entries) != 1 || entries[0].Text != "done" {
		t.Fatalf("status Snapshot() = %#v", entries)
	}
}

func TestProgressAllowsExplicitRegressionAndUnknownTotal(t *testing.T) {
	t.Parallel()

	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "scan", Label: "Scan", AllowRegression: true,
	})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	if err := progress.Update(10, "ten"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := progress.Update(2, "two"); err != nil {
		t.Fatalf("regressing Update() error = %v", err)
	}
	progress.Cancel("canceled")
	terminal := prompts.NewVirtualTerminal(40, 8)
	execution := prompts.Execution{
		Output:       terminal,
		Capabilities: prompts.Capabilities{OutputTerminal: true, Color: prompts.ColorANSI16},
	}
	if err := progress.Render(context.Background(), execution); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if output := terminal.Output(); !strings.Contains(output, "warning: Scan: 2 - canceled") || !strings.Contains(output, "\x1b[") {
		t.Fatalf("indeterminate output = %q", output)
	}
}

func TestProgressCalculatesExplicitClockRateAndETA(t *testing.T) {
	t.Parallel()

	clock := prompts.NewVirtualClock(time.Unix(100, 0))
	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "download", Label: "Download", Total: 10, Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	if err := progress.Update(0, "starting"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, ok := progress.Snapshot().RatePerSecond.Get(); ok {
		t.Fatal("first update invented a rate")
	}
	if err := clock.Advance(2 * time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if err := progress.Increment(4, "receiving"); err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	snapshot := progress.Snapshot()
	rate, rateOK := snapshot.RatePerSecond.Get()
	remaining, remainingOK := snapshot.EstimatedRemaining.Get()
	if !rateOK || rate != 2 || snapshot.Elapsed != 2*time.Second ||
		!remainingOK || remaining != 3*time.Second {
		t.Fatalf("Snapshot() = %#v, rate %v, eta %v", snapshot, rate, remaining)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	if err := progress.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if output := terminal.Output(); !strings.Contains(output, "2.00/s") || !strings.Contains(output, "eta 3s") {
		t.Fatalf("rate output = %q", output)
	}
}

func TestProgressRateHandlesUnknownTimeRegressionAndOverflow(t *testing.T) {
	t.Parallel()

	clock := prompts.NewVirtualClock(time.Time{})
	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "scan", Label: "Scan", AllowRegression: true, Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	_ = progress.Update(2, "two")
	_ = progress.Update(3, "same instant")
	if _, ok := progress.Snapshot().RatePerSecond.Get(); ok {
		t.Fatal("zero elapsed time produced a rate")
	}
	if err := clock.Advance(time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	_ = progress.Update(4, "four")
	if _, ok := progress.Snapshot().EstimatedRemaining.Get(); ok {
		t.Fatal("unknown total produced an ETA")
	}
	_ = progress.Update(1, "regressed")
	snapshot := progress.Snapshot()
	if _, ok := snapshot.RatePerSecond.Get(); ok || snapshot.Elapsed != 0 {
		t.Fatalf("regression retained rate state: %#v", snapshot)
	}

	slowClock := prompts.NewVirtualClock(time.Time{})
	slow, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "large", Label: "Large", Total: math.MaxInt64, Clock: slowClock,
	})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	_ = slow.Update(0, "starting")
	if err := slowClock.Advance(time.Hour); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	_ = slow.Update(1, "slow")
	if rate, ok := slow.Snapshot().RatePerSecond.Get(); !ok || rate <= 0 {
		t.Fatalf("slow rate = %v, %t", rate, ok)
	}
	if _, ok := slow.Snapshot().EstimatedRemaining.Get(); ok {
		t.Fatal("overflowing ETA was exposed")
	}
}

func TestProgressDefinitionsAndRenderingFailuresAreTyped(t *testing.T) {
	t.Parallel()

	for _, config := range []prompts.ProgressConfig{
		{}, {ID: "id"}, {ID: "id", Label: "Label", Total: -1},
	} {
		if _, err := prompts.NewProgress(config); !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewProgress(%#v) error = %v", config, err)
		}
	}
	progress, err := prompts.NewProgress(prompts.ProgressConfig{ID: "work", Label: "Work", Total: 1})
	if err != nil {
		t.Fatalf("NewProgress() error = %v", err)
	}
	if err := progress.Update(-1, "negative"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("negative update error = %v", err)
	}
	if err := progress.Increment(-1, "negative"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("negative increment error = %v", err)
	}
	//lint:ignore SA1012 Nil context behavior is part of the public contract.
	if err := progress.Render(nil, prompts.Execution{}); !errors.Is(err, prompts.ErrInvalidDefinition) { //nolint:staticcheck // contract test
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := progress.Render(ctx, prompts.Execution{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled render error = %v", err)
	}
	if err := progress.Render(context.Background(), prompts.Execution{}); !errors.Is(err, prompts.ErrWriter) {
		t.Fatalf("missing writer error = %v", err)
	}
	if err := progress.Render(context.Background(), prompts.Execution{Output: &failingWriter{err: io.ErrClosedPipe}}); !errors.Is(err, prompts.ErrWriter) {
		t.Fatalf("writer error = %v", err)
	}
	if err := progress.Render(context.Background(), prompts.Execution{
		Output: io.Discard,
		Renderer: rendererFunc(func(prompts.Frame, prompts.RenderOptions) (string, error) {
			return "", io.ErrClosedPipe
		}),
	}); !errors.Is(err, prompts.ErrRenderer) {
		t.Fatalf("renderer error = %v", err)
	}
	progress.Fail("failed")
	terminal := prompts.NewVirtualTerminal(40, 8)
	if err := progress.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil || !strings.Contains(terminal.Output(), "error: Work") {
		t.Fatalf("failed Render() = %v, output %q", err, terminal.Output())
	}
}

func TestSpinnerIsCallerDrivenAndReducedMotionSafe(t *testing.T) {
	t.Parallel()

	spinner, err := prompts.NewSpinner(prompts.SpinnerConfig{
		ID: "connect", Label: "Connect", Frames: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("NewSpinner() error = %v", err)
	}
	spinner.Advance("dialing")
	spinner.Advance("waiting")
	terminal := prompts.NewVirtualTerminal(40, 8)
	if err := spinner.Render(context.Background(), prompts.Execution{
		Output: terminal, Capabilities: prompts.Capabilities{Animation: true},
	}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if output := terminal.Output(); !strings.Contains(output, "one Connect - waiting") {
		t.Fatalf("spinner output = %q", output)
	}

	terminal = prompts.NewVirtualTerminal(40, 8)
	if err := spinner.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("reduced-motion Render() error = %v", err)
	}
	if output := terminal.Output(); strings.Contains(output, "one") || !strings.Contains(output, "Connect - waiting") {
		t.Fatalf("reduced-motion output = %q", output)
	}
	spinner.Succeed("connected")
	spinner.Fail("ignored")
	if snapshot := spinner.Snapshot(); snapshot.State != prompts.ProgressSucceeded || snapshot.Message != "connected" {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	spinner.Advance("ignored")
	if _, err := prompts.NewSpinner(prompts.SpinnerConfig{}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid spinner error = %v", err)
	}
	defaultSpinner, err := prompts.NewSpinner(prompts.SpinnerConfig{ID: "wait", Label: "Wait"})
	if err != nil || defaultSpinner.Snapshot().Frame != "-" {
		t.Fatalf("default spinner = %#v, %v", defaultSpinner, err)
	}

	states := []struct {
		name   string
		finish func(*prompts.Spinner)
		marker string
	}{
		{"success", func(value *prompts.Spinner) { value.Succeed("done") }, "success:"},
		{"failure", func(value *prompts.Spinner) { value.Fail("failed") }, "error:"},
		{"cancel", func(value *prompts.Spinner) { value.Cancel("stopped") }, "warning:"},
	}
	for _, state := range states {
		value, createErr := prompts.NewSpinner(prompts.SpinnerConfig{ID: state.name, Label: state.name})
		if createErr != nil {
			t.Fatalf("NewSpinner() error = %v", createErr)
		}
		state.finish(value)
		terminal = prompts.NewVirtualTerminal(40, 8)
		if renderErr := value.Render(context.Background(), prompts.Execution{Output: terminal}); renderErr != nil || !strings.Contains(terminal.Output(), state.marker) {
			t.Fatalf("%s Render() = %v, output %q", state.name, renderErr, terminal.Output())
		}
	}
}

func TestStatusStreamIsBoundedAndDeclarationOrdered(t *testing.T) {
	t.Parallel()

	stream, err := prompts.NewStatusStream(2)
	if err != nil {
		t.Fatalf("NewStatusStream() error = %v", err)
	}
	_ = stream.Append(prompts.StatusInfo, "one")
	_ = stream.Append(prompts.StatusWarning, "two")
	_ = stream.Append(prompts.StatusSuccess, "three")
	entries := stream.Snapshot()
	if len(entries) != 2 || entries[0].Text != "two" || entries[1].Text != "three" || stream.Dropped() != 1 {
		t.Fatalf("status stream = %#v, dropped %d", entries, stream.Dropped())
	}
	entries[0].Text = "changed"
	if stream.Snapshot()[0].Text != "two" {
		t.Fatal("Snapshot() exposed stream storage")
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	if err := stream.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if output := terminal.Output(); !strings.Contains(output, "warning: two") || !strings.Contains(output, "success: three") || !strings.Contains(output, "1 earlier status update omitted") {
		t.Fatalf("status output = %q", output)
	}
	if _, err := prompts.NewStatusStream(0); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid stream error = %v", err)
	}
	if err := stream.Append(prompts.StatusKind(200), "invalid"); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid status error = %v", err)
	}
	errorsOnly, err := prompts.NewStatusStream(1)
	if err != nil {
		t.Fatalf("NewStatusStream() error = %v", err)
	}
	_ = errorsOnly.Append(prompts.StatusError, "broken")
	terminal = prompts.NewVirtualTerminal(40, 8)
	if err := errorsOnly.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil || !strings.Contains(terminal.Output(), "error: broken") {
		t.Fatalf("error status Render() = %v, output %q", err, terminal.Output())
	}
	infoOnly, err := prompts.NewStatusStream(1)
	if err != nil {
		t.Fatalf("NewStatusStream() error = %v", err)
	}
	_ = infoOnly.Append(prompts.StatusInfo, "informational")
	terminal = prompts.NewVirtualTerminal(40, 8)
	if err := infoOnly.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil || !strings.Contains(terminal.Output(), "informational") {
		t.Fatalf("info status Render() = %v, output %q", err, terminal.Output())
	}
}
