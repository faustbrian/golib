package eventoutbox_test

import (
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/outbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
)

func TestNewStagerRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := eventoutbox.NewEnvelopeCodec(
		eventoutbox.FixedTopic("account-events"),
		eventoutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		writer *outboxpostgres.Writer
		codec  *eventoutbox.EnvelopeCodec
		want   error
	}{
		"transaction": {writer: writer, codec: codec, want: eventpostgres.ErrTransactionRequired},
		"writer":      {codec: codec, want: eventoutbox.ErrWriterRequired},
		"codec":       {writer: writer, want: eventoutbox.ErrCodecRequired},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := eventoutbox.NewStager(
				nil,
				eventpostgres.Config{},
				test.writer,
				test.codec,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewStager error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNilStagerFailsAsNotCommitted(t *testing.T) {
	t.Parallel()

	var stager *eventoutbox.Stager
	_, err := stager.Stage(
		t.Context(),
		eventsourcing.StreamID{},
		eventsourcing.ExpectedVersion{},
		nil,
	)
	if !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		eventsourcing.AppendCommitOutcome(err) != eventsourcing.CommitNotCommitted {
		t.Fatalf("Stage error = %v", err)
	}
}
