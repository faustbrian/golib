package eventsourcing_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestVerifyingReadersPreserveStoreContracts(t *testing.T) {
	t.Parallel()

	var verified atomic.Uint64
	verifier := eventsourcing.MessageVerifierFunc(func(
		context.Context,
		eventsourcing.Message,
	) error {
		verified.Add(1)

		return nil
	})
	if err := eventtest.CheckEventStore(
		context.Background(),
		func() (eventsourcing.EventStore, error) {
			return eventsourcing.NewVerifyingEventStore(
				memory.NewStore(),
				verifier,
			)
		},
	); err != nil {
		t.Fatalf("event-store conformance: %v", err)
	}
	if err := eventtest.CheckGlobalReader(
		context.Background(),
		func() (eventtest.GlobalEventStore, error) {
			store := memory.NewStore()
			streams, err := eventsourcing.NewVerifyingEventStore(
				store,
				verifier,
			)
			if err != nil {
				return nil, err
			}
			global, err := eventsourcing.NewVerifyingGlobalReader(
				store,
				verifier,
			)
			if err != nil {
				return nil, err
			}

			return verifyingGlobalStore{
				EventStore:   streams,
				GlobalReader: global,
			}, nil
		},
	); err != nil {
		t.Fatalf("global-reader conformance: %v", err)
	}
	if verified.Load() == 0 {
		t.Fatal("stored messages were not verified")
	}
}

func TestVerifyingEventStoreLeavesAppendSigningToTheApplication(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	verifierCalls := 0
	verified, err := eventsourcing.NewVerifyingEventStore(
		store,
		eventsourcing.MessageVerifierFunc(func(
			context.Context,
			eventsourcing.Message,
		) error {
			verifierCalls++

			return errors.New("read verification failure")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := eventsourcing.NewStreamID("account", "signed-write")
	if err != nil {
		t.Fatal(err)
	}
	pending := mustPendingForRepository(
		t,
		"signed-message",
		stream,
		mustEncodedEvent(
			t,
			"account.opened",
			1,
			[]byte(`{"owner":"Ada"}`),
		),
	)
	stored, err := verified.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil || len(stored) != 1 || verifierCalls != 0 {
		t.Fatalf(
			"Append() = %#v, %v verifier calls=%d",
			stored,
			err,
			verifierCalls,
		)
	}
}

func TestVerifyingReadersFailClosedBeforeExposingUntrustedMessages(
	t *testing.T,
) {
	t.Parallel()

	secret := errors.New("secret integrity diagnostic")
	tests := map[string]struct {
		verifier eventsourcing.MessageVerifier
		want     error
		cancel   bool
	}{
		"failure": {
			verifier: eventsourcing.MessageVerifierFunc(func(
				context.Context,
				eventsourcing.Message,
			) error {
				return secret
			}),
			want: secret,
		},
		"panic": {
			verifier: eventsourcing.MessageVerifierFunc(func(
				context.Context,
				eventsourcing.Message,
			) error {
				panic("secret verifier panic")
			}),
			want: eventsourcing.ErrMessageVerifierPanic,
		},
		"cancellation": {
			cancel: true,
			verifier: eventsourcing.MessageVerifierFunc(func(
				context.Context,
				eventsourcing.Message,
			) error {
				return nil
			}),
			want: context.Canceled,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, stream := integrityStore(t)
			ctx, cancel := context.WithCancel(context.Background())
			if testCase.cancel {
				testCase.verifier = eventsourcing.MessageVerifierFunc(func(
					context.Context,
					eventsourcing.Message,
				) error {
					cancel()

					return nil
				})
			}
			t.Cleanup(cancel)
			streams, err := eventsourcing.NewVerifyingEventStore(
				store,
				testCase.verifier,
			)
			if err != nil {
				t.Fatal(err)
			}
			global, err := eventsourcing.NewVerifyingGlobalReader(
				store,
				testCase.verifier,
			)
			if err != nil {
				t.Fatal(err)
			}
			streamOptions, err := eventsourcing.NewReadStreamOptions(
				eventsourcing.ReadStreamOptionsInput{
					FromVersion: 1,
					Limit:       1,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			globalOptions, err := eventsourcing.NewReadGlobalOptions(
				eventsourcing.ReadGlobalOptionsInput{
					FromPosition: 1,
					Limit:        1,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			streamIterator, err := streams.ReadStream(
				ctx,
				stream,
				streamOptions,
			)
			if err != nil {
				t.Fatal(err)
			}
			globalIterator, err := global.ReadGlobal(ctx, globalOptions)
			if err != nil {
				t.Fatal(err)
			}
			for _, iterator := range []eventsourcing.MessageIterator{
				streamIterator,
				globalIterator,
			} {
				if iterator.Next(ctx) ||
					!iterator.Message().ID().IsZero() ||
					!errors.Is(iterator.Err(), testCase.want) {
					t.Fatalf(
						"untrusted iterator = next true/message %#v/error %v",
						iterator.Message(),
						iterator.Err(),
					)
				}
				if testCase.want != context.Canceled &&
					(!errors.Is(
						iterator.Err(),
						eventsourcing.ErrMessageVerificationFailed,
					) ||
						strings.Contains(iterator.Err().Error(), "secret")) {
					t.Fatalf("verification error = %v", iterator.Err())
				}
				if iterator.Next(context.Background()) {
					t.Fatal("iterator resumed after verification failure")
				}
				if err := iterator.Close(); err != nil {
					t.Fatal(err)
				}
				if err := iterator.Close(); err != nil {
					t.Fatalf("second Close() error = %v", err)
				}
			}
		})
	}
}

func TestVerifyingReadersValidateComposition(t *testing.T) {
	t.Parallel()

	verifier := eventsourcing.MessageVerifierFunc(func(
		context.Context,
		eventsourcing.Message,
	) error {
		return nil
	})
	if store, err := eventsourcing.NewVerifyingEventStore(
		nil,
		verifier,
	); store != nil || !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewVerifyingEventStore(nil) = %#v, %v", store, err)
	}
	if store, err := eventsourcing.NewVerifyingEventStore(
		memory.NewStore(),
		nil,
	); store != nil || !errors.Is(
		err,
		eventsourcing.ErrMessageVerifierRequired,
	) {
		t.Fatalf("NewVerifyingEventStore(nil verifier) = %#v, %v", store, err)
	}
	if reader, err := eventsourcing.NewVerifyingGlobalReader(
		nil,
		verifier,
	); reader != nil || !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewVerifyingGlobalReader(nil) = %#v, %v", reader, err)
	}
	if reader, err := eventsourcing.NewVerifyingGlobalReader(
		memory.NewStore(),
		nil,
	); reader != nil || !errors.Is(
		err,
		eventsourcing.ErrMessageVerifierRequired,
	) {
		t.Fatalf("NewVerifyingGlobalReader(nil verifier) = %#v, %v", reader, err)
	}

	var nilVerifier eventsourcing.MessageVerifierFunc
	if err := nilVerifier.VerifyMessage(
		context.Background(),
		eventsourcing.Message{},
	); !errors.Is(err, eventsourcing.ErrMessageVerifierRequired) {
		t.Fatalf("nil verifier error = %v", err)
	}
}

type verifyingGlobalStore struct {
	eventsourcing.EventStore
	eventsourcing.GlobalReader
}

func integrityStore(t *testing.T) (*memory.Store, eventsourcing.StreamID) {
	t.Helper()

	store := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "integrity")
	if err != nil {
		t.Fatal(err)
	}
	pending := mustPendingForRepository(
		t,
		"integrity-message",
		stream,
		mustEncodedEvent(
			t,
			"account.opened",
			1,
			[]byte(`{"owner":"Ada"}`),
		),
	)
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}

	return store, stream
}
