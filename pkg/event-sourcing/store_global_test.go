package eventsourcing_test

import (
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestReadGlobalOptionsOwnBoundedInclusivePositions(t *testing.T) {
	t.Parallel()

	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 5,
			ToPosition:   9,
			Limit:        3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if options.FromPosition() != 5 ||
		options.ToPosition() != 9 ||
		options.Limit() != 3 ||
		!options.Valid() {
		t.Fatalf("NewReadGlobalOptions() = %#v", options)
	}

	openEnded, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        eventsourcing.MaxReadMessages,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if openEnded.ToPosition() != 0 || !openEnded.Valid() {
		t.Fatalf("open-ended options = %#v", openEnded)
	}
}

func TestReadGlobalOptionsRejectInvalidRanges(t *testing.T) {
	t.Parallel()

	tests := map[string]eventsourcing.ReadGlobalOptionsInput{
		"zero start": {},
		"reversed": {
			FromPosition: 2,
			ToPosition:   1,
			Limit:        1,
		},
		"zero limit": {
			FromPosition: 1,
		},
		"excessive limit": {
			FromPosition: 1,
			Limit:        eventsourcing.MaxReadMessages + 1,
		},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			options, err := eventsourcing.NewReadGlobalOptions(input)
			if !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
				options.Valid() {
				t.Fatalf("NewReadGlobalOptions() = %#v, %v", options, err)
			}
		})
	}
}
