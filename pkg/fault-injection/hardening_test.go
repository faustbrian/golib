package faultinject_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestConcurrentSelectionResetAndSnapshotRemainBounded(t *testing.T) {
	t.Parallel()

	rule := validRule("concurrent")
	rule.Maximum = 10_000
	rule.Schedule = faultinject.Probability(42, 1, 2)
	injector := injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{rule}})

	var wait sync.WaitGroup
	for worker := range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := range 200 {
				if (worker+iteration)%31 == 0 {
					injector.Reset()
				} else if (worker+iteration)%7 == 0 {
					if len(injector.Snapshot().Rules) != 1 {
						t.Error("snapshot lost bounded rule state")
					}
				} else {
					decision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
					if len(decision.Faults()) > 1 {
						t.Error("decision exceeded configured fault bound")
					}
				}
			}
		}()
	}
	wait.Wait()
}

func TestPodLocalSeedStreamsAreIndependentAndAmbientConfigurationIsInert(t *testing.T) {
	t.Setenv("FAULT_INJECTION", "enabled")

	configuration := faultinject.Config{Rules: []faultinject.Rule{{
		ID: "pod-local", Scope: faultinject.BoundaryFunction,
		Activation: faultinject.Active, Maximum: 64,
		Terminal: faultinject.Continue, Observation: faultinject.Suppress,
		Schedule: faultinject.Probability(41, 1, 2),
		Faults: []faultinject.Fault{
			faultinject.ErrorFault(faultinject.PhaseBefore, errInjected),
		},
	}}}
	left := injectorWithConfig(t, configuration)
	right := injectorWithConfig(t, configuration)
	differentConfiguration := configuration
	differentConfiguration.Rules = append([]faultinject.Rule(nil), configuration.Rules...)
	differentConfiguration.Rules[0].Schedule = faultinject.Probability(42, 1, 2)
	different := injectorWithConfig(t, differentConfiguration)

	var leftSequence, rightSequence, differentSequence []bool
	for range 32 {
		metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
		leftSequence = append(leftSequence, left.Decide(metadata).Injected())
		rightSequence = append(rightSequence, right.Decide(metadata).Injected())
		differentSequence = append(differentSequence, different.Decide(metadata).Injected())
	}
	if !equalBoolSequences(leftSequence, rightSequence) {
		t.Fatal("identical pod-local inputs produced different sequences")
	}
	if equalBoolSequences(leftSequence, differentSequence) {
		t.Fatal("different pod-local seeds unexpectedly produced the same sequence")
	}
	if left.Snapshot().Evaluations != 32 || right.Snapshot().Evaluations != 32 || different.Snapshot().Evaluations != 32 {
		t.Fatal("pod-local counters were not independently owned")
	}

	var disabled faultinject.Injector
	if os.Getenv("FAULT_INJECTION") != "enabled" || disabled.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected() {
		t.Fatal("ambient configuration activated the zero injector")
	}
}

func TestConcurrentAdapterCallsAndObserverDeliveryRemainAttributable(t *testing.T) {
	t.Parallel()

	var observed atomic.Uint64
	rule := validRule("concurrent-writer")
	rule.Scope = faultinject.BoundaryWriter
	rule.Maximum = 100_000
	rule.Observation = faultinject.Observe
	rule.Schedule = faultinject.Probability(73, 1, 2)
	injector := injectorWithConfig(t, faultinject.Config{
		Observer: faultinject.ObserverFunc(func(event faultinject.Event) {
			if event.RuleID != "concurrent-writer" || event.Boundary != faultinject.BoundaryWriter {
				t.Errorf("unexpected event attribution: %+v", event)
			}
			observed.Add(1)
		}),
		Rules: []faultinject.Rule{rule},
	})
	destination := &concurrentWriter{}
	writer := faultinject.WrapWriter(destination, injector, 91)

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				if _, err := writer.Write([]byte("x")); !errors.Is(err, errInjected) && err != nil {
					t.Errorf("unexpected write error: %v", err)
				}
			}
		}()
	}
	wait.Wait()
	snapshot := injector.Snapshot()
	if snapshot.Evaluations != 3_200 || observed.Load() != snapshot.Injections {
		t.Fatalf("concurrent attribution = evaluations %d, injections %d, events %d", snapshot.Evaluations, snapshot.Injections, observed.Load())
	}
}

func FuzzConfigurationNeverPanics(f *testing.F) {
	f.Add(uint8(1), uint64(1), uint64(1), uint64(2), int64(1), uint8(1))
	f.Add(uint8(255), uint64(0), uint64(0), uint64(0), int64(-1), uint8(0))
	f.Fuzz(func(t *testing.T, phase uint8, maximum, numerator, denominator uint64, delay int64, mask uint8) {
		rule := faultinject.Rule{
			ID: "fuzz", Scope: faultinject.BoundaryFunction,
			Activation: faultinject.Active, Maximum: maximum,
			Terminal: faultinject.Continue, Observation: faultinject.Suppress,
			Schedule: faultinject.Probability(7, numerator, denominator),
			Faults: []faultinject.Fault{
				faultinject.LatencyFault(faultinject.Phase(phase), durationFromFuzz(delay)),
				faultinject.ByteFault(faultinject.KindCorrupt, faultinject.Phase(phase), 1, mask),
			},
		}
		injector, err := faultinject.New(faultinject.Config{MaxLatency: 10, MaxBytes: 8, Rules: []faultinject.Rule{rule}})
		if err == nil {
			decision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
			if len(decision.Faults()) > 16 {
				t.Fatal("decision exceeded default fault bound")
			}
		}
	})
}

func FuzzByteAdaptersPreserveCallerBounds(f *testing.F) {
	f.Add([]byte("payload"), uint8(3), uint8(0x20))
	f.Add([]byte{}, uint8(1), uint8(1))
	f.Fuzz(func(t *testing.T, input []byte, rawLimit, mask uint8) {
		if len(input) > 4_096 {
			input = input[:4_096]
		}
		limit := int(rawLimit%32) + 1
		if mask == 0 {
			mask = 1
		}
		rule := validRule("bytes")
		rule.Scope = faultinject.BoundaryReader
		rule.Faults = []faultinject.Fault{faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, limit, mask)}
		injector, err := faultinject.New(faultinject.Config{MaxBytes: 32, Rules: []faultinject.Rule{rule}})
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, len(input))
		n, err := faultinject.WrapReader(bytes.NewReader(input), injector, 1).Read(buffer)
		if (err != nil && !errors.Is(err, io.EOF)) || n < 0 || n > len(buffer) {
			t.Fatalf("Read() = %d, %v for buffer %d", n, err, len(buffer))
		}
	})
}

func FuzzRuleSchedulesPredicatesAndCompositionRemainBounded(f *testing.F) {
	f.Add(uint8(0), uint64(7), uint64(1), uint32(2), uint64(3), uint8(3), true)
	f.Add(uint8(3), uint64(0), uint64(0), uint32(0), uint64(0), uint8(16), false)
	f.Fuzz(func(t *testing.T, scheduleKind uint8, seed, position uint64, operation uint32, attempt uint64, rawFaults uint8, repeat bool) {
		faultCount := int(rawFaults%16) + 1
		faults := make([]faultinject.Fault, faultCount)
		for index := range faults {
			faults[index] = faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)
		}
		var schedule faultinject.Schedule
		switch scheduleKind % 4 {
		case 0:
			schedule = faultinject.Every(position%16 + 1)
		case 1:
			schedule = faultinject.Nth(position%64 + 1)
		case 2:
			schedule = faultinject.Sequence([]bool{position&1 != 0, position&2 != 0, position&4 != 0}, repeat)
		default:
			denominator := position%64 + 1
			schedule = faultinject.Probability(seed, seed%(denominator+1), denominator)
		}
		rule := validRule("fuzz-composition")
		rule.Maximum = 128
		rule.Schedule = schedule
		rule.Faults = faults
		rule.Predicate = func(metadata faultinject.Metadata) bool {
			return metadata.Operation == operation && metadata.Attempt == attempt
		}
		injector, err := faultinject.New(faultinject.Config{MaxFaultsPerDecision: 16, Rules: []faultinject.Rule{rule}})
		if err != nil {
			t.Fatal(err)
		}
		for range 64 {
			decision := injector.Decide(faultinject.Metadata{
				Boundary: faultinject.BoundaryFunction, Operation: operation, Attempt: attempt,
			})
			if len(decision.Faults()) > faultCount || len(decision.Faults()) > 16 {
				t.Fatalf("decision fault count = %d", len(decision.Faults()))
			}
		}
	})
}

func FuzzWriterAdaptersPreserveCallerBounds(f *testing.F) {
	f.Add([]byte("payload"), uint8(2), uint8(0), uint8(1))
	f.Add([]byte{}, uint8(0), uint8(6), uint8(0xff))
	f.Fuzz(func(t *testing.T, input []byte, rawLimit, rawKind, mask uint8) {
		if len(input) > 4_096 {
			input = input[:4_096]
		}
		original := append([]byte(nil), input...)
		limit := int(rawLimit%32) + 1
		if mask == 0 {
			mask = 1
		}
		kinds := []faultinject.Kind{
			faultinject.KindCorrupt, faultinject.KindReorder,
			faultinject.KindShortWrite, faultinject.KindTruncate,
			faultinject.KindDuplicate, faultinject.KindDrop, faultinject.KindInterrupt,
		}
		kind := kinds[int(rawKind)%len(kinds)]
		phase := faultinject.PhaseDuring
		if kind == faultinject.KindDuplicate {
			phase = faultinject.PhaseAfter
		}
		rule := validRule("fuzz-writer")
		rule.Scope = faultinject.BoundaryWriter
		rule.Faults = []faultinject.Fault{faultinject.ByteFault(kind, phase, limit, mask)}
		injector, err := faultinject.New(faultinject.Config{MaxBytes: 32, Rules: []faultinject.Rule{rule}})
		if err != nil {
			t.Fatal(err)
		}
		var destination bytes.Buffer
		n, writeErr := faultinject.WrapWriter(&destination, injector, 1).Write(input)
		if n < 0 || n > len(input) || !bytes.Equal(input, original) || destination.Len() > len(input)+limit {
			t.Fatalf("Write() = %d, %v, destination=%d, input=%d", n, writeErr, destination.Len(), len(input))
		}
	})
}

func durationFromFuzz(value int64) time.Duration {
	return time.Duration(value)
}

func BenchmarkDisabledDecide(b *testing.B) {
	var injector faultinject.Injector
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
	b.ReportAllocs()
	for b.Loop() {
		injector.Decide(metadata)
	}
}

func BenchmarkNoMatchDecide(b *testing.B) {
	injector, _ := faultinject.New(faultinject.Config{})
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
	b.ReportAllocs()
	for b.Loop() {
		injector.Decide(metadata)
	}
}

func BenchmarkDeterministicMatch(b *testing.B) {
	rule := validRule("benchmark")
	rule.Maximum = 1_000_000_000
	injector := benchmarkInjector(b, faultinject.Config{Rules: []faultinject.Rule{rule}})
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		injector.Decide(metadata)
	}
}

func BenchmarkSeededProbability(b *testing.B) {
	rule := validRule("probability")
	rule.Maximum = 1_000_000_000
	rule.Schedule = faultinject.Probability(42, 1, 10)
	injector := benchmarkInjector(b, faultinject.Config{Rules: []faultinject.Rule{rule}})
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		injector.Decide(metadata)
	}
}

func BenchmarkObservedMatch(b *testing.B) {
	rule := validRule("observed")
	rule.Maximum = 1_000_000_000
	rule.Observation = faultinject.Observe
	injector := benchmarkInjector(b, faultinject.Config{
		Observer: faultinject.ObserverFunc(func(faultinject.Event) {}),
		Rules:    []faultinject.Rule{rule},
	})
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		injector.Decide(metadata)
	}
}

func BenchmarkCorruptReader(b *testing.B) {
	rule := validRule("corrupt")
	rule.Scope = faultinject.BoundaryReader
	rule.Maximum = 1_000_000_000
	rule.Faults = []faultinject.Fault{faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 64, 1)}
	injector := benchmarkInjector(b, faultinject.Config{Rules: []faultinject.Rule{rule}})
	reader := faultinject.WrapReader(repeatingReader{}, injector, 1)
	buffer := make([]byte, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = reader.Read(buffer)
	}
}

func BenchmarkInjectedLatency(b *testing.B) {
	b.Run("injected-sleeper", func(b *testing.B) {
		rule := validRule("latency")
		rule.Maximum = 1_000_000_000
		rule.Faults = []faultinject.Fault{faultinject.LatencyFault(faultinject.PhaseBefore, time.Nanosecond)}
		injector := benchmarkInjector(b, faultinject.Config{Sleeper: noOpBenchmarkSleeper{}, Rules: []faultinject.Rule{rule}})
		metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = faultinject.Run(context.Background(), injector, metadata, benchmarkSuccess)
		}
	})
	b.Run("system-timer", func(b *testing.B) {
		rule := validRule("timer")
		rule.Maximum = 1_000_000_000
		rule.Faults = []faultinject.Fault{faultinject.LatencyFault(faultinject.PhaseBefore, time.Nanosecond)}
		injector := benchmarkInjector(b, faultinject.Config{Rules: []faultinject.Rule{rule}})
		metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = faultinject.Run(context.Background(), injector, metadata, benchmarkSuccess)
		}
	})
}

func BenchmarkDirectErrorDouble(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = directErrorDouble(context.Background())
	}
}

func BenchmarkConcurrentSelectionContention(b *testing.B) {
	rule := validRule("parallel")
	rule.Maximum = 1_000_000_000
	injector := benchmarkInjector(b, faultinject.Config{Rules: []faultinject.Rule{rule}})
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			injector.Decide(metadata)
		}
	})
}

type repeatingReader struct{}

func (repeatingReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = byte(index)
	}
	return len(buffer), nil
}

type concurrentWriter struct {
	mu    sync.Mutex
	bytes int
}

func (writer *concurrentWriter) Write(buffer []byte) (int, error) {
	writer.mu.Lock()
	writer.bytes += len(buffer)
	writer.mu.Unlock()
	return len(buffer), nil
}

type noOpBenchmarkSleeper struct{}

func (noOpBenchmarkSleeper) Sleep(context.Context, time.Duration) error { return nil }

func benchmarkSuccess(context.Context) (struct{}, error) { return struct{}{}, nil }

func directErrorDouble(context.Context) (struct{}, error) { return struct{}{}, errInjected }

func equalBoolSequences(left, right []bool) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func benchmarkInjector(b *testing.B, configuration faultinject.Config) *faultinject.Injector {
	b.Helper()
	injector, err := faultinject.New(configuration)
	if err != nil {
		b.Fatal(err)
	}
	return injector
}
