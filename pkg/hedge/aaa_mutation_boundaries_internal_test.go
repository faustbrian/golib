package hedge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const mutationTestTimeout = 500 * time.Millisecond

type mutationResult struct {
	value  string
	report Report
	err    error
}

func runMutationDo(ctx context.Context, t *testing.T, policy *Policy[string], factory AttemptFactory[string]) mutationResult {
	t.Helper()
	done := make(chan mutationResult, 1)
	go func() {
		value, report, err := Do(ctx, policy, factory)
		done <- mutationResult{value: value, report: report, err: err}
	}()
	select {
	case result := <-done:
		return result
	case <-time.After(mutationTestTimeout):
		t.Fatal("Do did not complete within the behavioral deadline")
		return mutationResult{}
	}
}

func mutationConfig() Config[string] {
	budget, err := NewOutstandingBudget(4)
	if err != nil {
		panic(err)
	}
	return Config[string]{
		MaxHedges:      1,
		ReplaySafe:     true,
		Delay:          time.Hour,
		TotalTimeout:   200 * time.Millisecond,
		CleanupTimeout: 100 * time.Millisecond,
		Clock:          RealClock{},
		Budget:         budget,
		Classifier: ClassifyFunc[string](func(_ context.Context, result AttemptResult[string]) (Classification, error) {
			if result.Err != nil || result.ContextErr != nil {
				return ClassificationFailure, nil //nolint:nilerr // The classification carries the failure outcome.
			}
			return ClassificationSuccess, nil
		}),
		Disposer:           DisposeFunc[string](func(context.Context, string) error { return nil }),
		Resource:           "mutation-boundary",
		FactoryFailureMode: FactoryFailureStop,
	}
}

func mutationPolicy(t *testing.T, config Config[string]) *Policy[string] {
	t.Helper()
	policy, err := NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func waitMutationReport(t *testing.T, report Report) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), mutationTestTimeout)
	defer cancel()
	if err := report.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAAAPolicyBoundariesRemainInclusiveAndNilSafe(t *testing.T) {
	config := mutationConfig()
	config.MaxHedges = MaxHedges
	config.AttemptTimeout = config.TotalTimeout
	config.Resource = strings.Repeat("r", MaxResourceLength)
	config.Budget = fixedMutationBudget{capacity: MaxBudgetCapacity}
	if _, err := NewPolicy(config); err != nil {
		t.Fatalf("inclusive maxima rejected: %v", err)
	}

	var observer *mutationNilObserver
	config = mutationConfig()
	config.Observer = observer
	policy := mutationPolicy(t, config)
	if policy.config.Observer != nil {
		t.Fatal("typed nil observer was retained")
	}
	concreteObserver := &mutationNilObserver{}
	config = mutationConfig()
	config.Observer = concreteObserver
	policy = mutationPolicy(t, config)
	if policy.config.Observer != concreteObserver {
		t.Fatal("non-nil observer was discarded")
	}

	var nilChannel chan int
	var nilFunction func()
	var nilMap map[string]int
	var nilPointer *int
	var nilSlice []int
	for name, value := range map[string]any{
		"nil": nil, "channel": nilChannel, "function": nilFunction,
		"map": nilMap, "pointer": nilPointer, "slice": nilSlice,
	} {
		if !nilLike(value) {
			t.Fatalf("%s was not recognized as nil-like", name)
		}
	}
	number := 1
	for name, value := range map[string]any{
		"channel": make(chan int), "function": func() {}, "map": map[string]int{},
		"pointer": &number, "slice": []int{}, "scalar": 1, "struct": struct{}{},
	} {
		if nilLike(value) {
			t.Fatalf("%s was incorrectly recognized as nil-like", name)
		}
	}
}

type mutationNilObserver struct{}

func (*mutationNilObserver) TryObserve(Observation) bool { return true }

type fixedMutationBudget struct {
	capacity uint
	permit   Permit
	admitted bool
}

func (budget fixedMutationBudget) Capacity() uint { return budget.capacity }
func (budget fixedMutationBudget) TryAcquire(string) (Permit, bool) {
	return budget.permit, budget.admitted
}

func TestAAABudgetAndCleanupBoundariesTerminate(t *testing.T) {
	maximum, err := NewOutstandingBudget(MaxBudgetCapacity)
	if err != nil || maximum.Capacity() != MaxBudgetCapacity {
		t.Fatalf("maximum budget = (%v, %v)", maximum, err)
	}
	budget, err := NewOutstandingBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	permit, admitted := budget.TryAcquire("resource")
	if !admitted || permit == nil || budget.Outstanding() != 1 {
		t.Fatal("first budget admission failed")
	}
	if extra, ok := budget.TryAcquire("resource"); ok || extra != nil {
		t.Fatal("budget admitted work at its exact limit")
	}
	permit.Release()
	if budget.Outstanding() != 0 {
		t.Fatal("budget permit was not released")
	}

	cleanup := newCleanupState()
	cleanup.add()
	if cleanup.active != 1 {
		t.Fatalf("cleanup active after add = %d", cleanup.active)
	}
	cleanup.seal()
	select {
	case <-cleanup.done:
		t.Fatal("cleanup closed while work remained active")
	default:
	}
	cleanup.finish()
	if cleanup.active != 0 {
		t.Fatalf("cleanup active after finish = %d", cleanup.active)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mutationTestTimeout)
	defer cancel()
	if err := cleanup.wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAAAInternalSelectionAndRecoveryBoundaries(t *testing.T) {
	now := time.Unix(500, 0)
	normalFactory := AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "ok", nil }, "endpoint", nil
	})
	attempt, endpoint, err := safeFactory(context.Background(), normalFactory, AttemptInfo{})
	if err != nil || attempt == nil || endpoint != "endpoint" {
		t.Fatalf("safeFactory normal = (%v, %q, %v)", attempt, endpoint, err)
	}
	panicFactory := AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) { panic("private") })
	if attempt, endpoint, err = safeFactory(context.Background(), panicFactory, AttemptInfo{}); err == nil || attempt != nil || endpoint != "" {
		t.Fatalf("safeFactory panic = (%v, %q, %v)", attempt, endpoint, err)
	}
	if delay, err := safeDynamicDelay(func(DelayInput) (time.Duration, error) { return time.Second, nil }, DelayInput{}); delay != time.Second || err != nil {
		t.Fatalf("safeDynamicDelay normal = (%v, %v)", delay, err)
	}
	if delay, err := safeDynamicDelay(func(DelayInput) (time.Duration, error) { panic("private") }, DelayInput{}); delay != 0 || err == nil {
		t.Fatalf("safeDynamicDelay panic = (%v, %v)", delay, err)
	}

	equal := attemptCompletion[string]{result: AttemptResult[string]{Ordinal: 1}, completed: now}
	if completionLess(equal, equal) {
		t.Fatal("identical completions are not ordered")
	}
	current := attemptCompletion[string]{result: AttemptResult[string]{Ordinal: 2}, classification: ClassificationSuccess, completed: now}
	earlierSuccess := attemptCompletion[string]{result: AttemptResult[string]{Ordinal: 1}, classification: ClassificationSuccess, completed: now.Add(-time.Second)}
	earlierFailure := attemptCompletion[string]{result: AttemptResult[string]{Ordinal: 0}, classification: ClassificationFailure, completed: now.Add(-2 * time.Second)}
	earlierClassifierError := attemptCompletion[string]{result: AttemptResult[string]{Ordinal: 3}, classification: ClassificationSuccess, classificationErr: errors.New("classify"), completed: now.Add(-3 * time.Second)}
	winner, losers := chooseWinner(current, []attemptCompletion[string]{earlierFailure, earlierClassifierError, earlierSuccess}, nil)
	if winner.result.Ordinal != 1 || len(losers) != 3 {
		t.Fatalf("winner = %d, losers = %d", winner.result.Ordinal, len(losers))
	}

	nonSuccess := nonSuccesses([]attemptCompletion[string]{
		{result: AttemptResult[string]{Ordinal: 0}, classification: ClassificationSuccess},
		{result: AttemptResult[string]{Ordinal: 1}, classification: ClassificationFailure},
		{result: AttemptResult[string]{Ordinal: 2}, classification: ClassificationSuccess, classificationErr: errors.New("classify")},
		{result: AttemptResult[string]{Ordinal: 3}, classification: ClassificationTerminal},
	})
	if len(nonSuccess) != 3 || nonSuccess[0].result.Ordinal != 1 ||
		nonSuccess[1].result.Ordinal != 2 || nonSuccess[2].result.Ordinal != 3 {
		t.Fatalf("non-successes = %+v", nonSuccess)
	}
	metadata := failureMetadata([]attemptCompletion[string]{
		{result: AttemptResult[string]{Ordinal: 2}},
		{result: AttemptResult[string]{Ordinal: 0}},
		{result: AttemptResult[string]{Ordinal: 1}},
	})
	if metadata[0].Ordinal != 0 || metadata[1].Ordinal != 1 || metadata[2].Ordinal != 2 {
		t.Fatalf("failure metadata order = %+v", metadata)
	}
	selected := deterministicSelection([]attemptCompletion[string]{
		{result: AttemptResult[string]{Ordinal: 2}},
		{result: AttemptResult[string]{Ordinal: 0}},
		{result: AttemptResult[string]{Ordinal: 1}},
	})
	if selected.result.Ordinal != 0 {
		t.Fatalf("deterministic selection = %d", selected.result.Ordinal)
	}
	if selected = deterministicSelection[string](nil); selected != (attemptCompletion[string]{}) {
		t.Fatalf("empty deterministic selection = %+v", selected)
	}
	emit(nil, Observation{})
}

func TestAAADoImmediateAndFactoryBoundaries(t *testing.T) {
	config := mutationConfig()
	policy := mutationPolicy(t, config)
	validFactory := AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "winner", nil }, strings.Repeat("e", MaxResourceLength), nil
	})
	result := runMutationDo(context.Background(), t, policy, validFactory)
	if result.err != nil || result.value != "winner" || result.report.Reason != ReasonNoHedgeNeeded || result.report.AttemptsStarted != 1 {
		t.Fatalf("valid Do = (%q, %+v, %v)", result.value, result.report, result.err)
	}
	waitMutationReport(t, result.report)

	for name, factory := range map[string]AttemptFactory[string]{
		"nil attempt": AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) { return nil, "endpoint", nil }),
		"long endpoint": AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
			return func(context.Context) (string, error) { return "", nil }, strings.Repeat("e", MaxResourceLength+1), nil
		}),
		"factory error": AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
			return nil, "", errors.New("factory")
		}),
	} {
		result = runMutationDo(context.Background(), t, policy, factory)
		if result.err == nil || result.report.Reason != ReasonFactoryFailure || result.report.AttemptsStarted != 0 {
			t.Fatalf("%s Do = (%+v, %v)", name, result.report, result.err)
		}
		waitMutationReport(t, result.report)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := atomic.Bool{}
	result = runMutationDo(ctx, t, policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
		called.Store(true)
		return func(context.Context) (string, error) { return "unsafe", nil }, "endpoint", nil
	}))
	if !errors.Is(result.err, context.Canceled) || called.Load() || result.report.Reason != ReasonCallerCanceled {
		t.Fatalf("canceled Do = (%+v, %v), called=%v", result.report, result.err, called.Load())
	}
}

type mutationClock struct {
	mu       sync.Mutex
	timers   []*mutationTimer
	timeouts []time.Duration
}

func (*mutationClock) Now() time.Time { return time.Now() }

func (clock *mutationClock) NewTimer(time.Duration) Timer {
	timer := &mutationTimer{events: make(chan time.Time, 1)}
	clock.mu.Lock()
	clock.timers = append(clock.timers, timer)
	clock.mu.Unlock()
	return timer
}

func (clock *mutationClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	clock.mu.Lock()
	clock.timeouts = append(clock.timeouts, timeout)
	clock.mu.Unlock()
	return context.WithTimeout(parent, timeout)
}

func (clock *mutationClock) timer(t *testing.T, index int) *mutationTimer {
	t.Helper()
	deadline := time.Now().Add(mutationTestTimeout)
	for time.Now().Before(deadline) {
		clock.mu.Lock()
		if len(clock.timers) > index {
			timer := clock.timers[index]
			clock.mu.Unlock()
			return timer
		}
		clock.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timer %d was not created", index)
	return nil
}

func (clock *mutationClock) timeoutValues() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.timeouts...)
}

type mutationTimer struct {
	events  chan time.Time
	stopped atomic.Bool
}

func (timer *mutationTimer) C() <-chan time.Time { return timer.events }
func (timer *mutationTimer) Stop() bool          { return timer.stopped.CompareAndSwap(false, true) }

func TestAAAAttemptTimeoutAndTimerOwnershipBoundaries(t *testing.T) {
	clock := &mutationClock{}
	config := mutationConfig()
	config.Clock = clock
	policy := mutationPolicy(t, config)
	result := runMutationDo(context.Background(), t, policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "winner", nil }, "endpoint", nil
	}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	timeouts := clock.timeoutValues()
	if len(timeouts) != 1 || timeouts[0] != config.TotalTimeout {
		t.Fatalf("zero attempt timeout derived contexts = %v", timeouts)
	}
	if !clock.timer(t, 0).stopped.Load() {
		t.Fatal("winner did not stop the scheduled hedge timer")
	}
	waitMutationReport(t, result.report)

	clock = &mutationClock{}
	config = mutationConfig()
	config.Clock = clock
	config.AttemptTimeout = 50 * time.Millisecond
	policy = mutationPolicy(t, config)
	result = runMutationDo(context.Background(), t, policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "winner", nil }, "endpoint", nil
	}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	timeouts = clock.timeoutValues()
	if len(timeouts) != 2 || timeouts[0] != config.TotalTimeout || timeouts[1] != config.AttemptTimeout {
		t.Fatalf("attempt timeout derived contexts = %v", timeouts)
	}
	waitMutationReport(t, result.report)

	clock = &mutationClock{}
	config = mutationConfig()
	config.Clock = clock
	config.Classifier = ClassifyFunc[string](func(context.Context, AttemptResult[string]) (Classification, error) {
		return ClassificationTerminal, nil
	})
	policy = mutationPolicy(t, config)
	result = runMutationDo(context.Background(), t, policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "partial", errors.New("terminal") }, "endpoint", nil
	}))
	if result.err == nil || result.report.Reason != ReasonTerminalFailure || !clock.timer(t, 0).stopped.Load() {
		t.Fatalf("terminal Do = (%+v, %v), stopped=%v", result.report, result.err, clock.timer(t, 0).stopped.Load())
	}
	waitMutationReport(t, result.report)
}

func TestAAACancellationAndDeadlineStopTimers(t *testing.T) {
	for name, callerCancel := range map[string]bool{"caller": true, "deadline": false} {
		clock := &mutationClock{}
		config := mutationConfig()
		config.Clock = clock
		config.TotalTimeout = 30 * time.Millisecond
		policy := mutationPolicy(t, config)
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{}, 1)
		done := make(chan mutationResult, 1)
		go func() {
			value, report, err := Do(ctx, policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
				return func(attemptCtx context.Context) (string, error) {
					started <- struct{}{}
					<-attemptCtx.Done()
					return "", attemptCtx.Err()
				}, "endpoint", nil
			}))
			done <- mutationResult{value: value, report: report, err: err}
		}()
		select {
		case <-started:
		case <-time.After(mutationTestTimeout):
			t.Fatalf("%s attempt did not start", name)
		}
		timer := clock.timer(t, 0)
		if callerCancel {
			cancel()
		}
		select {
		case result := <-done:
			if callerCancel {
				if !errors.Is(result.err, context.Canceled) || result.report.Reason != ReasonCallerCanceled {
					t.Fatalf("caller result = (%+v, %v)", result.report, result.err)
				}
			} else if !errors.Is(result.err, context.DeadlineExceeded) || result.report.Reason != ReasonTotalDeadline {
				t.Fatalf("deadline result = (%+v, %v)", result.report, result.err)
			}
			if !timer.stopped.Load() {
				t.Fatalf("%s completion did not stop the timer", name)
			}
			waitMutationReport(t, result.report)
		case <-time.After(mutationTestTimeout):
			t.Fatalf("%s Do did not terminate", name)
		}
		cancel()
	}
}

func TestAAASchedulingAndAdmissionBoundaries(t *testing.T) {
	clock := &mutationClock{}
	config := mutationConfig()
	config.Clock = clock
	config.Delay = time.Second
	config.Budget = fixedMutationBudget{capacity: 1, permit: testPermit{}, admitted: true}
	policy := mutationPolicy(t, config)
	originalStarted := make(chan struct{}, 1)
	resultDone := make(chan mutationResult, 1)
	go func() {
		value, report, err := Do(context.Background(), policy, AttemptFactoryFunc[string](func(info AttemptInfo) (Attempt[string], string, error) {
			if !info.Hedge {
				return func(ctx context.Context) (string, error) {
					originalStarted <- struct{}{}
					<-ctx.Done()
					return "", ctx.Err()
				}, "original", nil
			}
			return func(context.Context) (string, error) { return "hedge", nil }, "hedge", nil
		}))
		resultDone <- mutationResult{value: value, report: report, err: err}
	}()
	<-originalStarted
	timer := clock.timer(t, 0)
	timer.events <- time.Now()
	select {
	case result := <-resultDone:
		if result.err != nil || result.value != "hedge" || result.report.AttemptsStarted != 2 || result.report.HedgesStarted != 1 || !timer.stopped.Load() {
			t.Fatalf("hedge result = (%q, %+v, %v)", result.value, result.report, result.err)
		}
		waitMutationReport(t, result.report)
	case <-time.After(mutationTestTimeout):
		t.Fatal("scheduled hedge did not complete")
	}

	for name, budget := range map[string]fixedMutationBudget{
		"nil permit":      {capacity: 1, admitted: true},
		"denied non-nil":  {capacity: 1, permit: testPermit{}, admitted: false},
		"admitted permit": {capacity: 1, permit: testPermit{}, admitted: true},
	} {
		clock = &mutationClock{}
		config = mutationConfig()
		config.Clock = clock
		config.Delay = time.Second
		config.Budget = budget
		policy = mutationPolicy(t, config)
		release := make(chan struct{})
		outcome := make(chan Outcome, 2)
		configObserver := mutationOutcomeObserver{events: outcome}
		policy.config.Observer = configObserver
		resultDone = make(chan mutationResult, 1)
		go func() {
			value, report, err := Do(context.Background(), policy, AttemptFactoryFunc[string](func(info AttemptInfo) (Attempt[string], string, error) {
				if !info.Hedge {
					return func(context.Context) (string, error) { <-release; return "original", nil }, "original", nil
				}
				return func(context.Context) (string, error) { return "hedge", nil }, "hedge", nil
			}))
			resultDone <- mutationResult{value: value, report: report, err: err}
		}()
		clock.timer(t, 0).events <- time.Now()
		var observed Outcome
		select {
		case observed = <-outcome:
		case <-time.After(mutationTestTimeout):
			close(release)
			t.Fatalf("%s produced no admission observation", name)
		}
		close(release)
		result := <-resultDone
		if name == "admitted permit" {
			if observed != OutcomeHedgeStarted || result.report.HedgesStarted != 1 {
				t.Fatalf("%s = outcome %v, report %+v", name, observed, result.report)
			}
		} else if observed != OutcomeBudgetDenied || result.report.BudgetDenied != 1 || result.report.HedgesStarted != 0 {
			t.Fatalf("%s = outcome %v, report %+v", name, observed, result.report)
		}
		waitMutationReport(t, result.report)
	}
}

type mutationOutcomeObserver struct{ events chan<- Outcome }

func (observer mutationOutcomeObserver) TryObserve(observation Observation) bool {
	if observation.Outcome == OutcomeBudgetDenied || observation.Outcome == OutcomeHedgeStarted {
		observer.events <- observation.Outcome
	}
	return true
}

func TestAAADelayAndFactoryFailureModesTerminate(t *testing.T) {
	for name, dynamic := range map[string]DelayFunc{
		"zero": func(DelayInput) (time.Duration, error) { return 0, nil },
		"error with positive delay": func(DelayInput) (time.Duration, error) {
			return time.Second, errors.New("delay")
		},
	} {
		config := mutationConfig()
		config.Delay = 0
		config.DynamicDelay = dynamic
		policy := mutationPolicy(t, config)
		result := runMutationDo(context.Background(), t, policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
			return func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }, "endpoint", nil
		}))
		if result.err == nil || result.report.Reason != ReasonDelayFailure {
			t.Fatalf("%s delay result = (%+v, %v)", name, result.report, result.err)
		}
		waitMutationReport(t, result.report)
	}

	clock := &mutationClock{}
	config := mutationConfig()
	config.Clock = clock
	config.MaxHedges = 2
	config.Delay = 0
	config.DynamicDelay = func(input DelayInput) (time.Duration, error) {
		if input.Hedge == 1 {
			return time.Second, nil
		}
		return time.Second, errors.New("second delay")
	}
	policy := mutationPolicy(t, config)
	started := make(chan struct{}, 2)
	done := make(chan mutationResult, 1)
	go func() {
		value, report, err := Do(context.Background(), policy, AttemptFactoryFunc[string](func(AttemptInfo) (Attempt[string], string, error) {
			return func(ctx context.Context) (string, error) {
				started <- struct{}{}
				<-ctx.Done()
				return "", ctx.Err()
			}, "endpoint", nil
		}))
		done <- mutationResult{value: value, report: report, err: err}
	}()
	<-started
	clock.timer(t, 0).events <- time.Now()
	<-started
	select {
	case result := <-done:
		if result.err == nil || result.report.Reason != ReasonDelayFailure || result.report.HedgesStarted != 1 {
			t.Fatalf("second delay result = (%+v, %v)", result.report, result.err)
		}
		waitMutationReport(t, result.report)
	case <-time.After(mutationTestTimeout):
		t.Fatal("second dynamic delay failure did not terminate")
	}

	for _, mode := range []FactoryFailureMode{FactoryFailureStop, FactoryFailureContinue} {
		clock := &mutationClock{}
		config := mutationConfig()
		config.Clock = clock
		config.Delay = time.Second
		config.FactoryFailureMode = mode
		policy := mutationPolicy(t, config)
		release := make(chan struct{})
		hedgeFactory := make(chan struct{}, 1)
		done := make(chan mutationResult, 1)
		go func() {
			value, report, err := Do(context.Background(), policy, AttemptFactoryFunc[string](func(info AttemptInfo) (Attempt[string], string, error) {
				if info.Hedge {
					hedgeFactory <- struct{}{}
					return nil, "", errors.New("factory")
				}
				return func(context.Context) (string, error) { <-release; return "original", nil }, "original", nil
			}))
			done <- mutationResult{value: value, report: report, err: err}
		}()
		clock.timer(t, 0).events <- time.Now()
		select {
		case <-hedgeFactory:
		case <-time.After(mutationTestTimeout):
			close(release)
			t.Fatal("hedge factory was not called")
		}
		if mode == FactoryFailureContinue {
			close(release)
		}
		select {
		case result := <-done:
			if mode == FactoryFailureStop {
				close(release)
				if result.err == nil || result.report.Reason != ReasonFactoryFailure {
					t.Fatalf("stop mode result = (%+v, %v)", result.report, result.err)
				}
			} else if result.err != nil || result.value != "original" || result.report.Reason != ReasonNoHedgeNeeded ||
				len(result.report.Failures) != 1 || result.report.Failures[0].Ordinal != 1 {
				t.Fatalf("continue mode result = (%q, %+v, %v)", result.value, result.report, result.err)
			}
			waitMutationReport(t, result.report)
		case <-time.After(mutationTestTimeout):
			close(release)
			t.Fatalf("factory mode %d did not terminate", mode)
		}
	}
}

type mutationBoundaryClock struct {
	created chan *mutationBlockingTimer
}

func newMutationBoundaryClock() *mutationBoundaryClock {
	return &mutationBoundaryClock{created: make(chan *mutationBlockingTimer, 3)}
}

func (*mutationBoundaryClock) Now() time.Time { return time.Now() }

func (clock *mutationBoundaryClock) NewTimer(time.Duration) Timer {
	timer := &mutationBlockingTimer{
		events:      make(chan time.Time, 1),
		stopCalled:  make(chan struct{}),
		releaseStop: make(chan struct{}),
	}
	clock.created <- timer
	return timer
}

func (*mutationBoundaryClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

type mutationBlockingTimer struct {
	events      chan time.Time
	stopCalled  chan struct{}
	releaseStop chan struct{}
	once        sync.Once
}

func (timer *mutationBlockingTimer) C() <-chan time.Time { return timer.events }
func (timer *mutationBlockingTimer) Stop() bool {
	stopped := false
	timer.once.Do(func() {
		stopped = true
		close(timer.stopCalled)
		<-timer.releaseStop
	})
	return stopped
}

type mutationCompletionObserver chan uint

func (observer mutationCompletionObserver) TryObserve(observation Observation) bool {
	if observation.Outcome == OutcomeAttemptCompleted {
		observer <- observation.Ordinal
	}
	return true
}

func TestAAATimerBoundaryDrainsEveryPublishedCompletionBeforeAdmission(t *testing.T) {
	clock := newMutationBoundaryClock()
	completed := make(mutationCompletionObserver, 2)
	config := mutationConfig()
	config.Clock = clock
	config.MaxHedges = 2
	config.Delay = time.Second
	budget, err := NewOutstandingBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	config.Budget = budget
	config.Observer = completed
	policy := mutationPolicy(t, config)
	releaseOriginal := make(chan struct{})
	releaseFirstHedge := make(chan struct{})
	firstHedgeStarted := make(chan struct{}, 1)
	secondHedgeStarted := atomic.Bool{}
	done := make(chan mutationResult, 1)
	go func() {
		value, report, doErr := Do(context.Background(), policy, AttemptFactoryFunc[string](func(info AttemptInfo) (Attempt[string], string, error) {
			switch info.Ordinal {
			case 0:
				return func(context.Context) (string, error) {
					<-releaseOriginal
					return "original", errors.New("failed")
				}, "original", nil
			case 1:
				firstHedgeStarted <- struct{}{}
				return func(context.Context) (string, error) {
					<-releaseFirstHedge
					return "winner", nil
				}, "hedge-1", nil
			default:
				secondHedgeStarted.Store(true)
				return func(context.Context) (string, error) { return "late", nil }, "hedge-2", nil
			}
		}))
		done <- mutationResult{value: value, report: report, err: doErr}
	}()

	firstTimer := <-clock.created
	firstTimer.events <- time.Now()
	<-firstTimer.stopCalled
	close(firstTimer.releaseStop)
	<-firstHedgeStarted
	secondTimer := <-clock.created
	secondTimer.events <- time.Now()
	<-secondTimer.stopCalled
	close(releaseOriginal)
	if ordinal := <-completed; ordinal != 0 {
		t.Fatalf("first published ordinal = %d", ordinal)
	}
	close(releaseFirstHedge)
	if ordinal := <-completed; ordinal != 1 {
		t.Fatalf("second published ordinal = %d", ordinal)
	}
	close(secondTimer.releaseStop)

	select {
	case result := <-done:
		if result.err != nil || result.value != "winner" || result.report.HedgesStarted != 1 || secondHedgeStarted.Load() {
			t.Fatalf("boundary result = (%q, %+v, %v), second hedge=%v", result.value, result.report, result.err, secondHedgeStarted.Load())
		}
		waitMutationReport(t, result.report)
	case <-time.After(mutationTestTimeout):
		t.Fatal("boundary execution did not terminate")
	}
}

func TestAAAPolicyDelayStrategiesRemainDistinct(t *testing.T) {
	fixed := mutationPolicy(t, mutationConfig())
	if delay, err := fixed.delay(1, 0); delay != time.Hour || err != nil {
		t.Fatalf("fixed delay = (%v, %v)", delay, err)
	}
	config := mutationConfig()
	config.Delay = 0
	config.Schedule = []time.Duration{2 * time.Second}
	scheduled := mutationPolicy(t, config)
	if delay, err := scheduled.delay(1, 0); delay != 2*time.Second || err != nil {
		t.Fatalf("scheduled delay = (%v, %v)", delay, err)
	}
	config = mutationConfig()
	config.Delay = 0
	config.DynamicDelay = func(input DelayInput) (time.Duration, error) { return input.Previous + time.Second, nil }
	dynamic := mutationPolicy(t, config)
	if delay, err := dynamic.delay(1, 2*time.Second); delay != 3*time.Second || err != nil {
		t.Fatalf("dynamic delay = (%v, %v)", delay, err)
	}
}
