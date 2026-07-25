package outbox_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
)

func TestObserverFuncForwardsEvent(t *testing.T) {
	t.Parallel()

	want := outbox.Event{Operation: outbox.OperationPublish, Outcome: outbox.OutcomeSuccess, Count: 1}
	var got outbox.Event
	observer := outbox.ObserverFunc(func(_ context.Context, event outbox.Event) { got = event })
	observer.Observe(context.Background(), want)
	if got != want {
		t.Fatalf("event = %#v, want %#v", got, want)
	}
}
