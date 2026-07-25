package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestReplayConfigAppliesBoundedDefaults(t *testing.T) {
	t.Parallel()

	config, err := normalizeReplayConfig(validReplayConfig())
	if err != nil {
		t.Fatalf("normalizeReplayConfig() error = %v", err)
	}
	if config.MaxPollRecords != 100 ||
		config.FetchMaxBytes != 50<<20 ||
		config.FetchMaxWait != 500*time.Millisecond ||
		config.HandlerTimeout != 30*time.Second ||
		config.DialTimeout != 10*time.Second {
		t.Fatalf("replay defaults = %#v", config)
	}
}

func TestReplayConfigRejectsInvalidRangesAndBounds(t *testing.T) {
	t.Parallel()

	manyRanges := make([]ReplayRange, 1_025)
	for index := range manyRanges {
		manyRanges[index] = ReplayRange{
			Topic:       "topic-" + strings.Repeat("x", index%200),
			Partition:   int32(index),
			StartOffset: 1,
			EndOffset:   2,
		}
	}
	tests := []struct {
		name   string
		change func(*ReplayConfig)
		want   error
	}{
		{name: "no broker", change: func(config *ReplayConfig) { config.Brokers = nil }, want: ErrBrokersRequired},
		{name: "no ranges", change: func(config *ReplayConfig) { config.Ranges = nil }, want: ErrReplayRangesRequired},
		{name: "too many ranges", change: func(config *ReplayConfig) { config.Ranges = manyRanges }, want: ErrTooManyReplayRanges},
		{name: "blank topic", change: func(config *ReplayConfig) { config.Ranges[0].Topic = " " }, want: ErrInvalidReplayRange},
		{name: "negative partition", change: func(config *ReplayConfig) { config.Ranges[0].Partition = -1 }, want: ErrInvalidReplayRange},
		{name: "negative start", change: func(config *ReplayConfig) { config.Ranges[0].StartOffset = -1 }, want: ErrInvalidReplayRange},
		{name: "empty range", change: func(config *ReplayConfig) { config.Ranges[0].EndOffset = 1 }, want: ErrInvalidReplayRange},
		{name: "duplicate range", change: func(config *ReplayConfig) {
			config.Ranges = append(config.Ranges, config.Ranges[0])
		}, want: ErrDuplicateReplayRange},
		{name: "insecure TLS", change: func(config *ReplayConfig) {
			config.Security.TLS = insecureTLSConfig()
		}, want: ErrInvalidSecurityConfig},
		{name: "excessive poll records", change: func(config *ReplayConfig) { config.MaxPollRecords = 1_001 }, want: ErrInvalidReplayConfig},
		{name: "excessive fetch bytes", change: func(config *ReplayConfig) { config.FetchMaxBytes = 101 << 20 }, want: ErrInvalidReplayConfig},
		{name: "excessive fetch wait", change: func(config *ReplayConfig) { config.FetchMaxWait = 31 * time.Second }, want: ErrInvalidReplayConfig},
		{name: "short handler timeout", change: func(config *ReplayConfig) { config.HandlerTimeout = time.Millisecond }, want: ErrInvalidReplayConfig},
		{name: "short dial timeout", change: func(config *ReplayConfig) { config.DialTimeout = time.Millisecond }, want: ErrInvalidReplayConfig},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validReplayConfig()
			test.change(&config)
			reader, err := NewReplayReader(config)
			if reader != nil {
				reader.Close()
				t.Fatal("NewReplayReader() returned reader for invalid config")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("NewReplayReader() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReplayProcessesExactRangesWithoutCommitting(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
		),
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 2},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 3},
		),
	}}
	reader := replayReaderWithBackend(backend, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 3,
	}})
	var offsets []int64

	result, err := reader.Replay(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		offsets = append(offsets, message.Offset)

		return nil
	}))

	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if result != (ReplayResult{Polled: 3, Processed: 2, CompletedRanges: 1}) ||
		len(offsets) != 2 || offsets[0] != 1 || offsets[1] != 2 ||
		backend.pollCalls != 2 {
		t.Fatalf("result/offsets/backend = %#v/%v/%#v", result, offsets, backend)
	}
}

func TestReplayStopsOnFetchHandlerAndConfigurationFailures(t *testing.T) {
	t.Parallel()

	reader := replayReaderWithBackend(&recordingReplayBackend{}, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
	}})
	if _, err := reader.Replay(context.Background(), nil); !errors.Is(err, ErrHandlerRequired) {
		t.Fatalf("Replay(nil) error = %v, want %v", err, ErrHandlerRequired)
	}

	fetchErr := errors.New("fetch failed")
	reader = replayReaderWithBackend(&recordingReplayBackend{
		fetches: []kgo.Fetches{kgo.NewErrFetch(fetchErr)},
	}, []ReplayRange{{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2}})
	if _, err := reader.Replay(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called after fetch error")

		return nil
	})); !errors.Is(err, fetchErr) {
		t.Fatalf("Replay() fetch error = %v, want %v", err, fetchErr)
	}

	handlerErr := errors.New("replay failed")
	reader = replayReaderWithBackend(&recordingReplayBackend{
		fetches: []kgo.Fetches{recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
		)},
	}, []ReplayRange{{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2}})
	if _, err := reader.Replay(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		return handlerErr
	})); !errors.Is(err, handlerErr) {
		t.Fatalf("Replay() handler error = %v, want %v", err, handlerErr)
	}
}

func TestReplayFailsClosedOnUnexpectedRecordsAndOffsetGaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record *kgo.Record
		want   error
	}{
		{
			name:   "unexpected partition",
			record: &kgo.Record{Topic: "events", Partition: 2, Offset: 1},
			want:   ErrUnexpectedReplayRecord,
		},
		{
			name:   "gap within range",
			record: &kgo.Record{Topic: "events", Partition: 1, Offset: 2},
			want:   ErrReplayOffsetGap,
		},
		{
			name:   "record before range",
			record: &kgo.Record{Topic: "events", Partition: 1, Offset: 0},
			want:   ErrReplayOffsetGap,
		},
		{
			name:   "record beyond range",
			record: &kgo.Record{Topic: "events", Partition: 1, Offset: 3},
			want:   ErrReplayOffsetGap,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader := replayReaderWithBackend(&recordingReplayBackend{
				fetches: []kgo.Fetches{recordFetches(test.record)},
			}, []ReplayRange{{
				Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 3,
			}})
			_, err := reader.Replay(context.Background(), HandlerFunc(func(
				context.Context,
				ConsumedMessage,
			) error {
				t.Fatal("handler called for invalid replay record")

				return nil
			}))
			if !errors.Is(err, test.want) {
				t.Fatalf("Replay() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReplayReaderConstructsClosesAndPreservesFactoryFailure(t *testing.T) {
	t.Parallel()

	reader, err := NewReplayReader(validReplayConfig())
	if err != nil {
		t.Fatalf("NewReplayReader() error = %v", err)
	}
	reader.Close()

	factoryErr := errors.New("client construction failed")
	reader, err = newReplayReader(validReplayConfig(), func(...kgo.Opt) (*kgo.Client, error) {
		return nil, factoryErr
	})
	if reader != nil {
		reader.Close()
		t.Fatal("newReplayReader() returned a reader after factory failure")
	}
	if !errors.Is(err, factoryErr) {
		t.Fatalf("newReplayReader() error = %v, want %v", err, factoryErr)
	}
}

func validReplayConfig() ReplayConfig {
	return ReplayConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track-replay",
		Ranges: []ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
	}
}

func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}

func replayReaderWithBackend(backend replayBackend, ranges []ReplayRange) *ReplayReader {
	return &ReplayReader{
		client:         backend,
		ranges:         ranges,
		maxPollRecords: 100,
		handlerTimeout: time.Second,
	}
}

type recordingReplayBackend struct {
	fetches   []kgo.Fetches
	pollCalls int
	closed    int
}

func (backend *recordingReplayBackend) PollRecords(
	ctx context.Context,
	_ int,
) kgo.Fetches {
	backend.pollCalls++
	if len(backend.fetches) == 0 {
		if err := ctx.Err(); err != nil {
			return kgo.NewErrFetch(err)
		}

		return kgo.NewErrFetch(errors.New("unexpected unscripted replay poll"))
	}
	fetches := backend.fetches[0]
	backend.fetches = backend.fetches[1:]

	return fetches
}

func (backend *recordingReplayBackend) Close() {
	backend.closed++
}
