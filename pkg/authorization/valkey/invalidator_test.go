package valkey

import (
	"context"
	"errors"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	native "github.com/valkey-io/valkey-go"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestNewValidatesOptions(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Options{}); !errors.Is(err, ErrNilClient) {
		t.Errorf("New(nil) error = %v, want ErrNilClient", err)
	}

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	if _, err := New(client, Options{Prefix: " "}); !errors.Is(err, ErrInvalidPrefix) {
		t.Errorf("New(blank prefix) error = %v, want ErrInvalidPrefix", err)
	}
	if _, err := New(client, Options{PollInterval: -time.Second}); !errors.Is(err, ErrInvalidPollInterval) {
		t.Errorf("New(negative interval) error = %v, want ErrInvalidPollInterval", err)
	}
	invalidator, err := New(client, Options{})
	if err != nil {
		t.Fatalf("New(defaults) error = %v", err)
	}
	if invalidator.key != "authorization:revision" ||
		invalidator.channel != "authorization:invalidate" ||
		invalidator.pollInterval != DefaultPollInterval {
		t.Errorf("New(defaults) = %+v", invalidator)
	}
}

func TestPublishAdvancesMonotonically(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{Prefix: "authz"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	client.EXPECT().Do(ctx, valkeymock.MatchFn(func(command []string) bool {
		return len(command) == 6 && command[0] == "EVAL" &&
			command[2] == "1" && command[3] == "authz:revision" &&
			command[4] == "authz:invalidate" && command[5] == "7"
	})).Return(valkeymock.Result(valkeymock.ValkeyInt64(1)))
	advanced, err := invalidator.Publish(ctx, 7)
	if err != nil || !advanced {
		t.Fatalf("Publish(7) = (%v, %v), want (true, nil)", advanced, err)
	}

	client.EXPECT().Do(ctx, gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyInt64(0)),
	)
	advanced, err = invalidator.Publish(ctx, 7)
	if err != nil || advanced {
		t.Fatalf("duplicate Publish(7) = (%v, %v), want (false, nil)", advanced, err)
	}
}

func TestPublishFailsClosed(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := invalidator.Publish(context.Background(), 0); !errors.Is(err, ErrInvalidRevision) {
		t.Errorf("Publish(0) error = %v, want ErrInvalidRevision", err)
	}

	backendError := errors.New("valkey unavailable")
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendError))
	if _, err := invalidator.Publish(context.Background(), 2); !errors.Is(err, backendError) {
		t.Errorf("failed Publish() error = %v, want backend error", err)
	}

	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyInt64(2)),
	)
	if _, err := invalidator.Publish(context.Background(), 2); !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("invalid Publish() response error = %v, want ErrInvalidResponse", err)
	}
}

func TestRevisionReadsDurableState(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{Prefix: "authz"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	client.EXPECT().Do(ctx, valkeymock.Match("GET", "authz:revision")).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("12")),
	)
	revision, err := invalidator.Revision(ctx)
	if err != nil || revision != 12 {
		t.Fatalf("Revision() = (%d, %v), want (12, nil)", revision, err)
	}

	client.EXPECT().Do(ctx, gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyNil()),
	)
	revision, err = invalidator.Revision(ctx)
	if err != nil || revision != 0 {
		t.Fatalf("empty Revision() = (%d, %v), want (0, nil)", revision, err)
	}
}

func TestRevisionRejectsUnavailableOrInvalidState(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	backendError := errors.New("valkey unavailable")
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendError))
	if _, err := invalidator.Revision(context.Background()); !errors.Is(err, backendError) {
		t.Errorf("failed Revision() error = %v, want backend error", err)
	}
	for _, value := range []string{"invalid", "0"} {
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(
			valkeymock.Result(valkeymock.ValkeyBlobString(value)),
		)
		if _, err := invalidator.Revision(context.Background()); !errors.Is(err, ErrInvalidRevision) {
			t.Errorf("Revision() for %q error = %v, want ErrInvalidRevision", value, err)
		}
	}
}

func TestWatchUsesDurableRevisionAfterPubSubWakeup(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{PollInterval: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := errors.New("stop watcher")
	client.EXPECT().Receive(gomock.Any(), valkeymock.Match("SUBSCRIBE", "authorization:invalidate"), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ native.Completed, handler func(native.PubSubMessage)) error {
			handler(native.PubSubMessage{Message: "untrusted payload"})
			handler(native.PubSubMessage{Message: "duplicate wakeup"})
			return nil
		},
	)
	first := client.EXPECT().Do(gomock.Any(), valkeymock.Match("GET", "authorization:revision")).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("2")),
	)
	client.EXPECT().Do(gomock.Any(), valkeymock.Match("GET", "authorization:revision")).After(first).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("3")),
	)
	err = invalidator.Watch(context.Background(), 2, func(revision authorization.Revision) error {
		if revision != 3 {
			t.Errorf("handler revision = %d, want 3", revision)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("Watch() error = %v, want handler error", err)
	}
}

func TestWatchStopsAfterSuccessfulHandler(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{PollInterval: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client.EXPECT().Receive(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ native.Completed, _ func(native.PubSubMessage)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	)
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("2")),
	)
	err = invalidator.Watch(ctx, 1, func(revision authorization.Revision) error {
		if revision != 2 {
			t.Errorf("handler revision = %d, want 2", revision)
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Watch() error = %v, want context.Canceled", err)
	}
}

func TestWatchPollsWhenPubSubFails(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	client.EXPECT().Receive(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		errors.New("pubsub unavailable"),
	)
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("1")),
	)
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("2")),
	)
	want := errors.New("observed by polling")
	err = invalidator.Watch(context.Background(), 1, func(revision authorization.Revision) error {
		if revision != 2 {
			t.Errorf("handler revision = %d, want 2", revision)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("Watch() error = %v, want polling handler error", err)
	}
}

func TestWatchValidatesInputsAndErrors(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	client := valkeymock.NewClient(controller)
	invalidator, err := New(client, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := invalidator.Watch(context.Background(), 1, nil); !errors.Is(err, ErrNilHandler) {
		t.Errorf("Watch(nil) error = %v, want ErrNilHandler", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := invalidator.Watch(ctx, 1, func(authorization.Revision) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("Watch(canceled) error = %v, want context.Canceled", err)
	}

	backendError := errors.New("valkey unavailable")
	client.EXPECT().Receive(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendError))
	if err := invalidator.Watch(context.Background(), 1, func(authorization.Revision) error { return nil }); !errors.Is(err, backendError) {
		t.Errorf("failed Watch() error = %v, want backend error", err)
	}
}
