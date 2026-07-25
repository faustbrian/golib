package eventsourcing_test

import (
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestExpectedVersionModesAreExplicit(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		expected eventsourcing.ExpectedVersion
		mode     eventsourcing.ExpectedVersionMode
		version  uint64
	}{
		"new": {
			expected: eventsourcing.ExpectNewStream(),
			mode:     eventsourcing.ExpectedVersionNew,
		},
		"existing": {
			expected: eventsourcing.ExpectExistingStream(),
			mode:     eventsourcing.ExpectedVersionExisting,
		},
		"exact": {
			expected: eventsourcing.ExpectExactVersion(42),
			mode:     eventsourcing.ExpectedVersionExact,
			version:  42,
		},
		"any": {
			expected: eventsourcing.ExpectAnyVersion(),
			mode:     eventsourcing.ExpectedVersionAny,
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if test.expected.Mode() != test.mode ||
				test.expected.Version() != test.version ||
				!test.expected.Valid() {
				t.Fatalf(
					"expected version = (%d, %d, %t)",
					test.expected.Mode(),
					test.expected.Version(),
					test.expected.Valid(),
				)
			}
		})
	}

	if (eventsourcing.ExpectedVersion{}).Valid() {
		t.Fatal("zero ExpectedVersion is valid")
	}
	if eventsourcing.ExpectExactVersion(0).Valid() {
		t.Fatal("exact zero ExpectedVersion is valid")
	}
}

func TestAppendErrorPreservesCauseAndCommitOutcome(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("write failed")
	err := eventsourcing.NewAppendError(eventsourcing.CommitNotCommitted, sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("append error = %v, want wrapped sentinel", err)
	}
	if eventsourcing.AppendCommitOutcome(err) != eventsourcing.CommitNotCommitted {
		t.Fatalf(
			"AppendCommitOutcome() = %d, want CommitNotCommitted",
			eventsourcing.AppendCommitOutcome(err),
		)
	}
	if eventsourcing.AppendCommitOutcome(sentinel) != eventsourcing.CommitUnknown {
		t.Fatalf(
			"unclassified outcome = %d, want CommitUnknown",
			eventsourcing.AppendCommitOutcome(sentinel),
		)
	}

	for outcome, message := range map[eventsourcing.CommitOutcome]string{
		eventsourcing.CommitUnknown:      "event append outcome is unknown",
		eventsourcing.CommitNotCommitted: "event append did not commit",
		eventsourcing.CommitCommitted:    "event append committed before a later failure",
	} {
		classified := eventsourcing.NewAppendError(outcome, sentinel)
		if classified.Error() != message {
			t.Fatalf("outcome %d error = %q, want %q", outcome, classified, message)
		}
	}
	if err := eventsourcing.NewAppendError(eventsourcing.CommitUnknown, nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewAppendError(nil) = %v, want ErrInvalidArgument", err)
	}
	if err := eventsourcing.NewAppendError(99, sentinel); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewAppendError(unknown) = %v, want ErrInvalidArgument", err)
	}
}

func TestReadStreamOptionsRequireBoundedForwardRange(t *testing.T) {
	t.Parallel()

	options, err := eventsourcing.NewReadStreamOptions(eventsourcing.ReadStreamOptionsInput{
		FromVersion: 4,
		ToVersion:   12,
		Limit:       5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.FromVersion() != 4 ||
		options.ToVersion() != 12 ||
		options.Limit() != 5 ||
		!options.Valid() {
		t.Fatalf(
			"read options = (%d, %d, %d, %t)",
			options.FromVersion(),
			options.ToVersion(),
			options.Limit(),
			options.Valid(),
		)
	}

	tests := map[string]eventsourcing.ReadStreamOptionsInput{
		"zero start": {Limit: 1},
		"zero limit": {FromVersion: 1},
		"limit too large": {
			FromVersion: 1,
			Limit:       eventsourcing.MaxReadMessages + 1,
		},
		"range reversed": {FromVersion: 2, ToVersion: 1, Limit: 1},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, buildErr := eventsourcing.NewReadStreamOptions(input); !errors.Is(
				buildErr,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewReadStreamOptions() error = %v", buildErr)
			}
		})
	}
	if (eventsourcing.ReadStreamOptions{}).Valid() {
		t.Fatal("zero ReadStreamOptions is valid")
	}
}

func TestConcurrencyErrorIsInspectableWithoutPrintingStreamIdentity(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("bank.account", "sensitive-account-id")
	if err != nil {
		t.Fatal(err)
	}
	conflict := &eventsourcing.ConcurrencyError{
		Stream:        stream,
		Expected:      eventsourcing.ExpectExactVersion(4),
		ActualVersion: 5,
	}
	if !errors.Is(conflict, eventsourcing.ErrConcurrencyConflict) {
		t.Fatalf("ConcurrencyError = %v, want ErrConcurrencyConflict", conflict)
	}
	if strings.Contains(conflict.Error(), stream.AggregateID()) {
		t.Fatalf("ConcurrencyError disclosed stream ID: %q", conflict)
	}
}
