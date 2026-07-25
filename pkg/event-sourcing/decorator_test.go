package eventsourcing_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestMessageDecoratorChainAppliesOrderedMetadataChanges(t *testing.T) {
	t.Parallel()

	_, original := pendingMessage(t, "message-1", "account.opened", 1, []byte("{}"))
	static := map[string]string{"source": "web"}
	first, err := eventsourcing.NewMetadataDecorator(static)
	if err != nil {
		t.Fatal(err)
	}
	static["source"] = "mutated"
	second := eventsourcing.MessageDecoratorFunc(
		func(message eventsourcing.PendingMessage) (eventsourcing.PendingMessage, error) {
			metadata := message.Metadata()
			if metadata["source"] != "web" {
				t.Fatalf("ordered metadata = %v", metadata)
			}
			metadata["region"] = "eu"

			return message.WithMetadata(metadata)
		},
	)
	chain, err := eventsourcing.NewMessageDecoratorChain(first, second)
	if err != nil {
		t.Fatal(err)
	}

	decorated, err := chain.Decorate(original)
	if err != nil {
		t.Fatal(err)
	}
	if got := decorated.Metadata(); got["source"] != "web" || got["region"] != "eu" {
		t.Fatalf("decorated metadata = %v", got)
	}
	if original.Metadata()["source"] != "" {
		t.Fatalf("original metadata changed: %v", original.Metadata())
	}
	if decorated.ID() != original.ID() ||
		decorated.Stream() != original.Stream() ||
		decorated.RecordedAt() != original.RecordedAt() ||
		decorated.Event().Name() != original.Event().Name() ||
		decorated.Event().Version() != original.Event().Version() ||
		decorated.Event().ContentType() != original.Event().ContentType() ||
		!bytes.Equal(decorated.Event().Payload(), original.Event().Payload()) {
		t.Fatal("decoration changed immutable envelope identity")
	}

	returned := decorated.Metadata()
	returned["source"] = "caller-mutated"
	if decorated.Metadata()["source"] != "web" {
		t.Fatal("decorated metadata aliases caller map")
	}
}

func TestMetadataDecoratorRejectsCollisionsAndInvalidMetadata(t *testing.T) {
	t.Parallel()

	_, original := pendingMessage(t, "message-1", "account.opened", 1, []byte("{}"))
	withExisting, err := original.WithMetadata(map[string]string{"source": "api"})
	if err != nil {
		t.Fatal(err)
	}
	decorator, err := eventsourcing.NewMetadataDecorator(map[string]string{"source": "worker"})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := eventsourcing.NewMessageDecoratorChain(decorator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Decorate(withExisting); !errors.Is(
		err,
		eventsourcing.ErrMetadataCollision,
	) {
		t.Fatalf("Decorate(collision) error = %v", err)
	}

	if _, err := eventsourcing.NewMetadataDecorator(
		map[string]string{"es.reserved": "value"},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewMetadataDecorator(reserved) error = %v", err)
	}
	if _, err := original.WithMetadata(
		map[string]string{"es.reserved": "value"},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("WithMetadata(reserved) error = %v", err)
	}
	if _, err := (eventsourcing.PendingMessage{}).WithMetadata(nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("zero WithMetadata() error = %v", err)
	}
}

func TestMessageDecoratorChainContainsFailuresAndInvalidChanges(t *testing.T) {
	t.Parallel()

	_, original := pendingMessage(t, "message-1", "account.opened", 1, []byte("{}"))
	secretFailure := errors.New("credential-secret")
	cases := map[string]struct {
		decorator eventsourcing.MessageDecoratorFunc
		want      error
	}{
		"error": {
			decorator: func(eventsourcing.PendingMessage) (eventsourcing.PendingMessage, error) {
				return eventsourcing.PendingMessage{}, secretFailure
			},
			want: secretFailure,
		},
		"panic": {
			decorator: func(eventsourcing.PendingMessage) (eventsourcing.PendingMessage, error) {
				panic("private-panic-value")
			},
			want: eventsourcing.ErrDecoratorPanic,
		},
		"zero output": {
			decorator: func(eventsourcing.PendingMessage) (eventsourcing.PendingMessage, error) {
				return eventsourcing.PendingMessage{}, nil
			},
			want: eventsourcing.ErrInvalidArgument,
		},
		"identity change": {
			decorator: func(eventsourcing.PendingMessage) (eventsourcing.PendingMessage, error) {
				_, changed := pendingMessage(
					t,
					"message-2",
					"account.opened",
					1,
					[]byte("{}"),
				)

				return changed, nil
			},
			want: eventsourcing.ErrDecoratorChangedMessage,
		},
	}

	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			chain, err := eventsourcing.NewMessageDecoratorChain(testCase.decorator)
			if err != nil {
				t.Fatal(err)
			}
			_, err = chain.Decorate(original)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Decorate() error = %v, want %v", err, testCase.want)
			}
			if strings.Contains(err.Error(), "credential-secret") ||
				strings.Contains(err.Error(), "private-panic-value") {
				t.Fatalf("Decorate() disclosed private diagnostic: %q", err)
			}
			var decoratorErr *eventsourcing.DecoratorError
			if !errors.As(err, &decoratorErr) || decoratorErr.Index != 0 {
				t.Fatalf("DecoratorError = %#v", decoratorErr)
			}
		})
	}
}

func TestMessageDecoratorChainValidatesConstructionAndInput(t *testing.T) {
	t.Parallel()

	var nilDecorator eventsourcing.MessageDecoratorFunc
	if _, err := eventsourcing.NewMessageDecoratorChain(nilDecorator); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewMessageDecoratorChain(nil) error = %v", err)
	}
	chain, err := eventsourcing.NewMessageDecoratorChain()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Decorate(eventsourcing.PendingMessage{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Decorate(zero) error = %v", err)
	}
	var nilChain *eventsourcing.MessageDecoratorChain
	_, original := pendingMessage(t, "message-1", "account.opened", 1, []byte("{}"))
	if _, err := nilChain.Decorate(original); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil Decorate() error = %v", err)
	}
	decorated, err := chain.Decorate(original)
	if err != nil {
		t.Fatal(err)
	}
	if decorated.ID() != original.ID() {
		t.Fatal("empty chain changed message")
	}
}

func pendingMessage(
	t *testing.T,
	id string,
	name string,
	version eventsourcing.SchemaVersion,
	payload []byte,
) (eventsourcing.StreamID, eventsourcing.PendingMessage) {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        name,
		Version:     version,
		ContentType: eventsourcing.JSONContentType,
		Payload:     payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         id,
		Stream:     stream,
		Event:      event,
		RecordedAt: time.Date(2026, time.July, 25, 9, 30, 45, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	return stream, message
}
