package prompts

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProgressState identifies a stable lifecycle state.
type ProgressState uint8

const (
	ProgressPending ProgressState = iota
	ProgressRunning
	ProgressSucceeded
	ProgressFailed
	ProgressCanceled
)

// ProgressConfig defines determinate or indeterminate caller-owned progress.
// A zero total is indeterminate.
type ProgressConfig struct {
	ID, Label       string
	Total           int64
	AllowRegression bool
	Clock           Clock
}

// ProgressSnapshot is an immutable state copy.
type ProgressSnapshot struct {
	ID, Label, Message string
	Current, Total     int64
	State              ProgressState
	Elapsed            time.Duration
	RatePerSecond      Optional[float64]
	EstimatedRemaining Optional[time.Duration]
}

// Progress stores only the latest update and never creates a goroutine or
// queue. Update is non-blocking with respect to output; Render is explicit.
type Progress struct {
	mu              sync.RWMutex
	snapshot        ProgressSnapshot
	allowRegression bool
	clock           Clock
	measured        bool
	measuredAt      time.Time
	measuredCurrent int64
}

// NewProgress creates a caller-driven progress value.
func NewProgress(config ProgressConfig) (*Progress, error) {
	if config.ID == "" || config.Label == "" || config.Total < 0 {
		return nil, invalidBehaviorDefinition("define progress", config.ID, fmt.Errorf("%w: progress identity, label, and total are invalid", ErrInvalidDefinition))
	}
	return &Progress{
		snapshot:        ProgressSnapshot{ID: config.ID, Label: config.Label, Total: config.Total, State: ProgressPending},
		allowRegression: config.AllowRegression,
		clock:           config.Clock,
	}, nil
}

// Update replaces the latest progress value and message.
func (progress *Progress) Update(current int64, message string) error {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if terminalProgressState(progress.snapshot.State) || current < 0 ||
		(!progress.allowRegression && current < progress.snapshot.Current) ||
		(progress.snapshot.Total > 0 && current > progress.snapshot.Total) {
		return invalidBehaviorDefinition("update progress", progress.snapshot.ID, ErrInvalidDefinition)
	}
	progress.snapshot.Current = current
	progress.snapshot.Message = message
	progress.snapshot.State = ProgressRunning
	progress.updateTiming(current)
	return nil
}

// UpdateContext replaces the latest progress value unless the context has
// ended. Prefer it when updates are emitted from caller-owned operations.
func (progress *Progress) UpdateContext(ctx context.Context, current int64, message string) error {
	if err := presentationMutationContext(ctx, progress.snapshot.ID); err != nil {
		return err
	}
	return progress.Update(current, message)
}

// Increment atomically advances progress without integer overflow.
func (progress *Progress) Increment(delta int64, message string) error {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if terminalProgressState(progress.snapshot.State) || delta < 0 || progress.snapshot.Current > math.MaxInt64-delta {
		return invalidBehaviorDefinition("increment progress", progress.snapshot.ID, ErrInvalidDefinition)
	}
	next := progress.snapshot.Current + delta
	if progress.snapshot.Total > 0 && next > progress.snapshot.Total {
		return invalidBehaviorDefinition("increment progress", progress.snapshot.ID, ErrInvalidDefinition)
	}
	progress.snapshot.Current = next
	progress.snapshot.Message = message
	progress.snapshot.State = ProgressRunning
	progress.updateTiming(next)
	return nil
}

// IncrementContext atomically advances progress unless the context has ended.
func (progress *Progress) IncrementContext(ctx context.Context, delta int64, message string) error {
	if err := presentationMutationContext(ctx, progress.snapshot.ID); err != nil {
		return err
	}
	return progress.Increment(delta, message)
}

func (progress *Progress) updateTiming(current int64) {
	if progress.clock == nil {
		return
	}
	now := progress.clock.Now()
	if !progress.measured || current < progress.measuredCurrent {
		progress.measured = true
		progress.measuredAt = now
		progress.measuredCurrent = current
		progress.clearTiming()
		return
	}
	elapsed := now.Sub(progress.measuredAt)
	delta := current - progress.measuredCurrent
	progress.clearTiming()
	if elapsed <= 0 || delta <= 0 {
		return
	}
	rate := float64(delta) / elapsed.Seconds()
	progress.snapshot.Elapsed = elapsed
	progress.snapshot.RatePerSecond = Some(rate)
	if progress.snapshot.Total <= 0 {
		return
	}
	etaNanos := float64(progress.snapshot.Total-current) / rate * float64(time.Second)
	if etaNanos < 0 || etaNanos > float64(math.MaxInt64) {
		return
	}
	progress.snapshot.EstimatedRemaining = Some(time.Duration(etaNanos))
}

func (progress *Progress) clearTiming() {
	progress.snapshot.Elapsed = 0
	progress.snapshot.RatePerSecond = Optional[float64]{}
	progress.snapshot.EstimatedRemaining = Optional[time.Duration]{}
}

// Complete records a stable successful terminal state.
func (progress *Progress) Complete(message string) { progress.finish(ProgressSucceeded, message) }

// Fail records a stable failed terminal state.
func (progress *Progress) Fail(message string) { progress.finish(ProgressFailed, message) }

// Cancel records a stable canceled terminal state.
func (progress *Progress) Cancel(message string) { progress.finish(ProgressCanceled, message) }

func (progress *Progress) finish(state ProgressState, message string) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if terminalProgressState(progress.snapshot.State) {
		return
	}
	progress.snapshot.State = state
	progress.snapshot.Message = message
}

func terminalProgressState(state ProgressState) bool {
	return state == ProgressSucceeded || state == ProgressFailed || state == ProgressCanceled
}

// Snapshot returns the latest coalesced state.
func (progress *Progress) Snapshot() ProgressSnapshot {
	progress.mu.RLock()
	defer progress.mu.RUnlock()
	return progress.snapshot
}

// Render explicitly writes the latest stable semantic line.
func (progress *Progress) Render(ctx context.Context, execution Execution) error {
	snapshot := progress.Snapshot()
	role := RoleProgress
	switch snapshot.State {
	case ProgressPending, ProgressRunning:
		role = RoleProgress
	case ProgressSucceeded:
		role = RoleSuccess
	case ProgressFailed:
		role = RoleError
	case ProgressCanceled:
		role = RoleWarning
	}
	text := snapshot.Label + ": " + strconv.FormatInt(snapshot.Current, 10)
	if snapshot.Total > 0 {
		percent := math.Round(float64(snapshot.Current) / float64(snapshot.Total) * 100)
		text += fmt.Sprintf("/%d (%.0f%%)", snapshot.Total, percent)
	}
	if rate, ok := snapshot.RatePerSecond.Get(); ok {
		text += fmt.Sprintf(" @ %.2f/s", rate)
	}
	if estimate, ok := snapshot.EstimatedRemaining.Get(); ok {
		text += " (eta " + estimate.String() + ")"
	}
	if snapshot.Message != "" {
		text += " - " + snapshot.Message
	}
	return renderOutput(ctx, snapshot.ID, NewFrame(Line(Text(role, text))), execution)
}

// SpinnerConfig defines caller-advanced indeterminate status frames.
type SpinnerConfig struct {
	ID, Label string
	Frames    []string
}

// SpinnerSnapshot is an immutable spinner state copy.
type SpinnerSnapshot struct {
	ProgressSnapshot
	Frame string
}

// Spinner advances only when the caller requests it and creates no timer.
type Spinner struct {
	mu       sync.RWMutex
	snapshot ProgressSnapshot
	frames   []string
	index    int
}

// NewSpinner creates a caller-driven spinner.
func NewSpinner(config SpinnerConfig) (*Spinner, error) {
	if config.ID == "" || config.Label == "" {
		return nil, invalidBehaviorDefinition("define spinner", config.ID, ErrInvalidDefinition)
	}
	frames := append([]string(nil), config.Frames...)
	if len(frames) == 0 {
		frames = []string{"-", "\\", "|", "/"}
	}
	return &Spinner{
		snapshot: ProgressSnapshot{ID: config.ID, Label: config.Label, State: ProgressPending},
		frames:   frames,
	}, nil
}

// Advance selects the next frame and coalesces the latest message.
func (spinner *Spinner) Advance(message string) {
	spinner.mu.Lock()
	defer spinner.mu.Unlock()
	if terminalProgressState(spinner.snapshot.State) {
		return
	}
	spinner.index = (spinner.index + 1) % len(spinner.frames)
	spinner.snapshot.State = ProgressRunning
	spinner.snapshot.Message = message
}

// AdvanceContext selects the next frame unless the context has ended.
func (spinner *Spinner) AdvanceContext(ctx context.Context, message string) error {
	if err := presentationMutationContext(ctx, spinner.snapshot.ID); err != nil {
		return err
	}
	spinner.Advance(message)
	return nil
}

func (spinner *Spinner) Succeed(message string) { spinner.finish(ProgressSucceeded, message) }
func (spinner *Spinner) Fail(message string)    { spinner.finish(ProgressFailed, message) }
func (spinner *Spinner) Cancel(message string)  { spinner.finish(ProgressCanceled, message) }

func (spinner *Spinner) finish(state ProgressState, message string) {
	spinner.mu.Lock()
	defer spinner.mu.Unlock()
	if terminalProgressState(spinner.snapshot.State) {
		return
	}
	spinner.snapshot.State = state
	spinner.snapshot.Message = message
}

// Snapshot returns the latest state and caller-selected frame.
func (spinner *Spinner) Snapshot() SpinnerSnapshot {
	spinner.mu.RLock()
	defer spinner.mu.RUnlock()
	return SpinnerSnapshot{ProgressSnapshot: spinner.snapshot, Frame: spinner.frames[spinner.index]}
}

// Render writes a frame only when animation is explicitly supported.
func (spinner *Spinner) Render(ctx context.Context, execution Execution) error {
	snapshot := spinner.Snapshot()
	role := RoleProgress
	switch snapshot.State {
	case ProgressPending, ProgressRunning:
		role = RoleProgress
	case ProgressSucceeded:
		role = RoleSuccess
	case ProgressFailed:
		role = RoleError
	case ProgressCanceled:
		role = RoleWarning
	}
	parts := []string{}
	if execution.Capabilities.Animation && !terminalProgressState(snapshot.State) {
		parts = append(parts, snapshot.Frame)
	}
	parts = append(parts, snapshot.Label)
	text := strings.Join(parts, " ")
	if snapshot.Message != "" {
		text += " - " + snapshot.Message
	}
	return renderOutput(ctx, snapshot.ID, NewFrame(Line(Text(role, text))), execution)
}

// StatusKind identifies one bounded line-oriented status role.
type StatusKind uint8

const (
	StatusInfo StatusKind = iota
	StatusWarning
	StatusError
	StatusSuccess
)

// StatusEntry is a deterministic status-stream value.
type StatusEntry struct {
	Kind StatusKind
	Text string
}

// StatusStream retains only its configured number of latest entries.
type StatusStream struct {
	mu       sync.RWMutex
	capacity int
	entries  []StatusEntry
	dropped  uint64
}

// NewStatusStream creates a bounded concurrent stream.
func NewStatusStream(capacity int) (*StatusStream, error) {
	if capacity < 1 {
		return nil, invalidBehaviorDefinition("define status stream", "", ErrInvalidDefinition)
	}
	return &StatusStream{capacity: capacity, entries: make([]StatusEntry, 0, capacity)}, nil
}

// Append adds one entry and drops the oldest entry at capacity.
func (stream *StatusStream) Append(kind StatusKind, text string) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if kind > StatusSuccess {
		return invalidBehaviorDefinition("append status", "status", ErrInvalidDefinition)
	}
	if len(stream.entries) == stream.capacity {
		copy(stream.entries, stream.entries[1:])
		stream.entries = stream.entries[:len(stream.entries)-1]
		stream.dropped++
	}
	stream.entries = append(stream.entries, StatusEntry{Kind: kind, Text: text})
	return nil
}

// AppendContext adds one entry unless the context has ended.
func (stream *StatusStream) AppendContext(ctx context.Context, kind StatusKind, text string) error {
	if err := presentationMutationContext(ctx, "status"); err != nil {
		return err
	}
	return stream.Append(kind, text)
}

// Snapshot returns entries in append order.
func (stream *StatusStream) Snapshot() []StatusEntry {
	stream.mu.RLock()
	defer stream.mu.RUnlock()
	return append([]StatusEntry(nil), stream.entries...)
}

// Dropped returns the number of omitted oldest entries.
func (stream *StatusStream) Dropped() uint64 {
	stream.mu.RLock()
	defer stream.mu.RUnlock()
	return stream.dropped
}

// Render writes a deterministic linear stream snapshot.
func (stream *StatusStream) Render(ctx context.Context, execution Execution) error {
	entries := stream.Snapshot()
	lines := make([]SemanticLine, 0, len(entries)+1)
	if dropped := stream.Dropped(); dropped > 0 {
		lines = append(lines, Line(Text(RoleHint, fmt.Sprintf("%d earlier status update omitted", dropped))))
	}
	for _, entry := range entries {
		role := RoleValue
		switch entry.Kind {
		case StatusInfo:
			role = RoleValue
		case StatusWarning:
			role = RoleWarning
		case StatusError:
			role = RoleError
		case StatusSuccess:
			role = RoleSuccess
		}
		lines = append(lines, Line(Text(role, entry.Text)))
	}
	return renderOutput(ctx, "status", NewFrame(lines...), execution)
}

func renderOutput(ctx context.Context, identity string, frame Frame, execution Execution) error {
	if ctx == nil {
		return invalidBehaviorDefinition("render output", identity, ErrInvalidDefinition)
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(identity, err)
	}
	if execution.Output == nil {
		return streamFailure(identity, ErrorWriter, "write output", ErrWriter)
	}
	renderer := execution.Renderer
	if renderer == nil {
		renderer = PlainRenderer{Theme: execution.Theme}
		if execution.Capabilities.Color != ColorNone {
			renderer = ANSIRenderer{Theme: execution.Theme}
		}
	}
	output, err := renderer.Render(frame, RenderOptions{
		Width: execution.Capabilities.Width, Color: execution.Capabilities.Color,
		ASCIIOnly:  !execution.Capabilities.Unicode,
		Hyperlinks: execution.Capabilities.Hyperlinks,
	})
	if err != nil {
		return streamFailure(identity, ErrorRenderer, "render output", err)
	}
	if _, err = io.WriteString(execution.Output, output); err != nil {
		return streamFailure(identity, ErrorWriter, "write output", err)
	}
	return nil
}

func presentationMutationContext(ctx context.Context, identity string) error {
	if ctx == nil {
		return invalidBehaviorDefinition("update presentation", identity, ErrInvalidDefinition)
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(identity, err)
	}
	return nil
}
