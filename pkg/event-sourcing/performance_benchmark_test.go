package eventsourcing_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	benchmarkLifecycleState int
	benchmarkDecodedEvent   eventsourcing.DecodedEvent
	benchmarkSnapshotState  snapshotBenchmarkState
	benchmarkUpcastEvents   []eventsourcing.UpcastEvent
)

type benchmarkCounterIncremented struct {
	Amount     int               `json:"amount"`
	Sequence   int64             `json:"sequence"`
	OccurredAt time.Time         `json:"occurred_at"`
	Labels     map[string]string `json:"labels"`
}

type snapshotBenchmarkState struct {
	Count int `json:"count"`
}

func BenchmarkLifecycleReconstitution(benchmark *testing.B) {
	for _, length := range []int{10, 100, 1_000, 10_000} {
		history := benchmarkHistory(benchmark, length)
		benchmark.Run(strconv.Itoa(length), func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			benchmark.ResetTimer()
			var lifecycle eventsourcing.Lifecycle
			var err error
			for benchmark.Loop() {
				lifecycle = eventsourcing.Lifecycle{}
				benchmarkLifecycleState = 0
				err = lifecycle.Reconstitute(
					0,
					history,
					func(event eventsourcing.DecodedEvent) error {
						benchmarkLifecycleState += event.Value().(benchmarkCounterIncremented).Amount
						return nil
					},
				)
			}
			benchmark.StopTimer()
			if err != nil {
				benchmark.Fatal(err)
			}
			if benchmarkLifecycleState != length ||
				lifecycle.CommittedVersion() != uint64(length) {
				benchmark.Fatalf(
					"reconstitution = state %d, version %d",
					benchmarkLifecycleState,
					lifecycle.CommittedVersion(),
				)
			}
		})
	}
}

func BenchmarkJSONCodecRoundTrip(benchmark *testing.B) {
	codec, decoded := benchmarkJSONFixture(benchmark)
	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	var err error
	for benchmark.Loop() {
		encoded, encodeErr := codec.Encode(decoded)
		if encodeErr != nil {
			err = encodeErr
			break
		}
		benchmarkDecodedEvent, err = codec.Decode(encoded)
		if err != nil {
			break
		}
	}
	benchmark.StopTimer()
	if err != nil {
		benchmark.Fatal(err)
	}
	value := benchmarkDecodedEvent.Value().(benchmarkCounterIncremented)
	if value.Sequence != 9_007_199_254_740_993 || value.Labels["a"] != "first" {
		benchmark.Fatalf("round trip = %#v", value)
	}
}

func BenchmarkSnapshotRestoreBreakEven(benchmark *testing.B) {
	for _, total := range []int{100, 1_000, 10_000} {
		fullHistory := benchmarkHistory(benchmark, total)
		benchmark.Run(fmt.Sprintf("%d/full-history", total), func(benchmark *testing.B) {
			benchmarkSnapshotRestore(benchmark, 0, nil, fullHistory, total)
		})
		for _, percent := range []int{10, 25, 50, 75, 90} {
			snapshotVersion := total * percent / 100
			tail := benchmarkHistoryFrom(
				benchmark,
				snapshotVersion,
				total-snapshotVersion,
			)
			state, err := json.Marshal(snapshotBenchmarkState{Count: snapshotVersion})
			if err != nil {
				benchmark.Fatal(err)
			}
			benchmark.Run(
				fmt.Sprintf("%d/snapshot-%d-percent", total, percent),
				func(benchmark *testing.B) {
					benchmarkSnapshotRestore(
						benchmark,
						snapshotVersion,
						state,
						tail,
						total,
					)
				},
			)
		}
	}
}

func BenchmarkUpcasterChain(benchmark *testing.B) {
	for _, depth := range []int{1, 4, 16} {
		chain, input := benchmarkUpcasterFixture(benchmark, depth)
		benchmark.Run(strconv.Itoa(depth), func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			benchmark.ResetTimer()
			var err error
			for benchmark.Loop() {
				benchmarkUpcastEvents, err = chain.Upcast(input)
			}
			benchmark.StopTimer()
			if err != nil {
				benchmark.Fatal(err)
			}
			if len(benchmarkUpcastEvents) != 1 ||
				benchmarkUpcastEvents[0].Event().Version() != eventsourcing.SchemaVersion(depth+1) {
				benchmark.Fatalf("upcast depth %d = %#v", depth, benchmarkUpcastEvents)
			}
		})
	}
}

func TestPerformanceBenchmarkFixtures(t *testing.T) {
	t.Parallel()

	history := benchmarkHistory(t, 3)
	var lifecycle eventsourcing.Lifecycle
	state := 0
	if err := lifecycle.Reconstitute(0, history, func(event eventsourcing.DecodedEvent) error {
		state += event.Value().(benchmarkCounterIncremented).Amount
		return nil
	}); err != nil || state != 3 || lifecycle.CommittedVersion() != 3 {
		t.Fatalf("lifecycle fixture = state %d, version %d, error %v", state, lifecycle.CommittedVersion(), err)
	}

	codec, decoded := benchmarkJSONFixture(t)
	encoded, err := codec.Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(encoded); err != nil {
		t.Fatal(err)
	}

	chain, input := benchmarkUpcasterFixture(t, 3)
	output, err := chain.Upcast(input)
	if err != nil || len(output) != 1 || output[0].Event().Version() != 4 {
		t.Fatalf("upcaster fixture = %#v, %v", output, err)
	}

	tail := benchmarkHistoryFrom(t, 9, 1)
	snapshotState := snapshotBenchmarkState{Count: 9}
	var restored eventsourcing.Lifecycle
	if err := restored.Reconstitute(9, tail, func(event eventsourcing.DecodedEvent) error {
		snapshotState.Count += event.Value().(benchmarkCounterIncremented).Amount
		return nil
	}); err != nil || snapshotState.Count != 10 || restored.CommittedVersion() != 10 {
		t.Fatalf(
			"snapshot fixture = state %d, version %d, error %v",
			snapshotState.Count,
			restored.CommittedVersion(),
			err,
		)
	}
}

func benchmarkHistory(t testing.TB, length int) []eventsourcing.HistoricalEvent {
	t.Helper()
	return benchmarkHistoryFrom(t, 0, length)
}

func benchmarkHistoryFrom(
	t testing.TB,
	baseVersion int,
	length int,
) []eventsourcing.HistoricalEvent {
	t.Helper()
	decoded, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "benchmark.counter.incremented",
		Version: 1,
		Value:   benchmarkCounterIncremented{Amount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	history := make([]eventsourcing.HistoricalEvent, length)
	for index := range history {
		history[index], err = eventsourcing.NewHistoricalEvent(
			eventsourcing.HistoricalEventInput{
				SourceVersion: uint64(baseVersion + index + 1),
				SegmentCount:  1,
				Event:         decoded,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	return history
}

func benchmarkSnapshotRestore(
	benchmark *testing.B,
	baseVersion int,
	encodedState []byte,
	history []eventsourcing.HistoricalEvent,
	want int,
) {
	benchmark.Helper()
	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	var lifecycle eventsourcing.Lifecycle
	var err error
	for benchmark.Loop() {
		lifecycle = eventsourcing.Lifecycle{}
		benchmarkSnapshotState = snapshotBenchmarkState{}
		if encodedState != nil {
			if err = json.Unmarshal(encodedState, &benchmarkSnapshotState); err != nil {
				break
			}
		}
		err = lifecycle.Reconstitute(
			uint64(baseVersion),
			history,
			func(event eventsourcing.DecodedEvent) error {
				benchmarkSnapshotState.Count += event.Value().(benchmarkCounterIncremented).Amount
				return nil
			},
		)
		if err != nil {
			break
		}
	}
	benchmark.StopTimer()
	if err != nil {
		benchmark.Fatal(err)
	}
	if benchmarkSnapshotState.Count != want || lifecycle.CommittedVersion() != uint64(want) {
		benchmark.Fatalf(
			"restoration = state %d, version %d",
			benchmarkSnapshotState.Count,
			lifecycle.CommittedVersion(),
		)
	}
}

func benchmarkJSONFixture(
	t testing.TB,
) (*eventsourcing.JSONCodec, eventsourcing.DecodedEvent) {
	t.Helper()
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[benchmarkCounterIncremented](
			"benchmark.counter.incremented",
			1,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "benchmark.counter.incremented",
		Version: 1,
		Value: benchmarkCounterIncremented{
			Amount:     1,
			Sequence:   9_007_199_254_740_993,
			OccurredAt: time.Date(2026, time.January, 1, 0, 0, 0, 123456000, time.UTC),
			Labels:     map[string]string{"z": "last", "a": "first"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return codec, decoded
}

func benchmarkUpcasterFixture(
	t testing.TB,
	depth int,
) (*eventsourcing.UpcasterChain, eventsourcing.UpcastEvent) {
	t.Helper()
	rules := make([]eventsourcing.UpcastRule, depth)
	for index := range rules {
		target := eventsourcing.SchemaVersion(index + 2)
		rule, err := eventsourcing.NewUpcastRule(
			"benchmark.counter.incremented",
			eventsourcing.SchemaVersion(index+1),
			func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
				current := input.Event()
				next, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
					Name:        current.Name().String(),
					Version:     target,
					ContentType: current.ContentType(),
					Payload:     current.Payload(),
				})
				if err != nil {
					return nil, err
				}
				upcast, err := eventsourcing.NewUpcastEvent(next, input.Metadata())
				if err != nil {
					return nil, err
				}
				return []eventsourcing.UpcastEvent{upcast}, nil
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		rules[index] = rule
	}
	chain, err := eventsourcing.NewUpcasterChain(rules...)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "benchmark.counter.incremented",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte(`{"amount":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := eventsourcing.NewUpcastEvent(
		encoded,
		map[string]string{"benchmark": fmt.Sprintf("depth-%d", depth)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return chain, input
}
