package breaker_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func FuzzConfigurationNeverPanics(f *testing.F) {
	f.Add(uint16(10), uint16(5), uint64(0x3fe0000000000000), int64(time.Second), byte(0))
	f.Add(uint16(0), uint16(0), math.Float64bits(math.NaN()), int64(-1), byte(255))
	f.Fuzz(func(t *testing.T, size, minimum uint16, ratioBits uint64, durationNanos int64, kind byte) {
		config := breaker.Config{
			Name:              "fuzz",
			MinimumThroughput: int(minimum),
			Opening: &breaker.OpeningRules{
				FailureRatio: math.Float64frombits(ratioBits),
			},
			OpenDuration: breaker.FixedOpenDuration(time.Duration(durationNanos)),
		}
		if kind%2 == 0 {
			config.Window = breaker.CountWindow{Size: int(size)}
		} else {
			config.Window = breaker.TimeWindow{
				BucketDuration: time.Duration(durationNanos),
				BucketCount:    int(size),
			}
		}
		b, err := breaker.New(config)
		if err == nil {
			_ = b.Close()
		}
	})
}

func FuzzPermitOperationSequences(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{0, 0, 1, 1, 2, 2, 4, 4})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 1024 {
			t.Skip()
		}
		clock := breakertest.NewClock(time.Unix(100, 0))
		b, err := breaker.New(breaker.Config{
			Name:              "fuzz",
			Clock:             clock,
			MinimumThroughput: 2,
			Opening:           &breaker.OpeningRules{FailureRatio: 0.5},
			OpenDuration:      breaker.FixedOpenDuration(time.Second),
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:         2,
				RequiredSuccesses: 2,
			},
			PermitTTL: time.Second,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = b.Close() }()
		var permits []*breaker.Permit
		for _, operation := range operations {
			switch operation % 12 {
			case 0:
				permit, acquireErr := b.Acquire(context.Background())
				if acquireErr == nil {
					permits = append(permits, permit)
				}
			case 1, 2, 3:
				if len(permits) > 0 {
					index := int(operation) % len(permits)
					_ = permits[index].Complete(breaker.Outcome(operation%3), operation&1 == 1)
				}
			case 4:
				if len(permits) > 0 {
					_ = permits[int(operation)%len(permits)].Cancel()
				}
			case 5:
				clock.Advance(time.Duration(operation%5) * time.Second)
			case 6:
				_ = b.Reset()
			case 7:
				_ = b.Disable()
			case 8:
				_ = b.Release()
			case 9:
				_ = b.ForceOpen()
			case 10:
				_ = b.Isolate()
			case 11:
				_ = b.Snapshot()
			}
			snapshot := b.Snapshot()
			assertSnapshotInvariants(t, snapshot, 2)
		}
	})
}

func FuzzObserverSequences(f *testing.F) {
	f.Add(byte(0), []byte{0, 1, 2, 3})
	f.Add(byte(3), []byte{4, 5, 6, 7})
	f.Fuzz(func(t *testing.T, behavior byte, operations []byte) {
		if len(operations) > 256 {
			t.Skip()
		}
		var circuit *breaker.Breaker
		observer := func(event breaker.TransitionEvent) error {
			switch behavior % 4 {
			case 0:
				_ = circuit.Snapshot()
			case 1:
				panic("observer panic")
			case 2:
				return errors.New("observer unavailable")
			case 3:
				if event.After.Mode == breaker.ModeNormal {
					_ = circuit.ForceOpen()
				} else {
					_ = circuit.Snapshot()
				}
			}
			return nil
		}
		var err error
		circuit, err = breaker.New(breaker.Config{
			Name:              "observer-fuzz",
			MinimumThroughput: 1,
			Opening:           &breaker.OpeningRules{FailureCount: 1},
			Observer:          observer,
			EventDelivery:     breaker.SynchronousEvents{},
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		for _, operation := range operations {
			switch operation % 6 {
			case 0:
				permit, acquireErr := circuit.Acquire(context.Background())
				if acquireErr == nil {
					_ = permit.Complete(breaker.OutcomeFailure, operation&1 == 1)
				}
			case 1:
				_ = circuit.Reset()
			case 2:
				_ = circuit.Disable()
			case 3:
				_ = circuit.Isolate()
			case 4:
				_ = circuit.Release()
			case 5:
				_ = circuit.Snapshot()
			}
			snapshot := circuit.Snapshot()
			if snapshot.Generation == 0 || snapshot.ObserverFailures > snapshot.TransitionCount {
				t.Fatalf("invalid Snapshot() = %+v", snapshot)
			}
		}
	})
}

func FuzzConfigurationResourceBounds(f *testing.F) {
	f.Add([]byte("inventory"), uint32(100), uint32(10), uint64(50), uint32(10), uint32(64), byte(0))
	f.Add([]byte{}, uint32(math.MaxUint32), uint32(math.MaxUint32), uint64(math.MaxUint64), uint32(math.MaxUint32), uint32(math.MaxUint32), byte(255))
	f.Fuzz(func(
		t *testing.T,
		name []byte,
		windowSize uint32,
		minimum uint32,
		failureCount uint64,
		probes uint32,
		eventBuffer uint32,
		kind byte,
	) {
		if len(name) > 4096 {
			t.Skip()
		}
		config := breaker.Config{
			Name:              string(name),
			Window:            breaker.CountWindow{Size: int(windowSize)},
			MinimumThroughput: int(minimum),
			Opening:           &breaker.OpeningRules{FailureCount: failureCount},
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:         int(probes),
				RequiredSuccesses: 1,
			},
		}
		if kind&1 != 0 {
			config.Observer = func(breaker.TransitionEvent) error { return nil }
			config.EventDelivery = breaker.AsynchronousEvents{
				Buffer:   int(eventBuffer),
				Overflow: breaker.EventOverflowPolicy(kind >> 1),
			}
		}
		circuit, err := breaker.New(config)
		if err == nil {
			_ = circuit.Shutdown(context.Background())
		}
	})
}

func FuzzExecutionDurationsAndOutcomes(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Fuzz(func(t *testing.T, sequence []byte) {
		if len(sequence) > 1024 {
			t.Skip()
		}
		clock := breakertest.NewClock(time.Unix(100, 0))
		circuit, err := breaker.New(breaker.Config{
			Name:              "execution-fuzz",
			Clock:             clock,
			Window:            breaker.CountWindow{Size: 1024},
			MinimumThroughput: 1,
			Opening: &breaker.OpeningRules{
				ConsecutiveFailures: math.MaxUint64,
			},
			SlowCallDuration: time.Millisecond,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		for _, operation := range sequence {
			_, _ = breaker.Execute(context.Background(), circuit, func(context.Context) (byte, error) {
				clock.Advance(time.Duration(int8(operation)) * time.Millisecond)
				switch operation % 3 {
				case 0:
					return operation, nil
				case 1:
					return operation, errors.New("dependency failure")
				default:
					return operation, context.Canceled
				}
			})
		}
		snapshot := circuit.Snapshot()
		if snapshot.WindowClassified != uint64(len(sequence)) ||
			snapshot.Successes+snapshot.Failures != snapshot.WindowClassified {
			t.Fatalf("invalid Snapshot() = %+v", snapshot)
		}
	})
}
