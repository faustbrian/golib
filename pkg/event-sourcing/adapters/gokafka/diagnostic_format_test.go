package gokafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestExportedErrorsRedactRetainedCausesForEveryFormatter(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("credential=formatter-secret")
	codec, err := NewRecordCodec(RecordCodecConfig{
		Resolver: TopicResolverFunc(func(eventsourcing.Message) (string, error) {
			return "", sensitive
		}),
		AllowedTopics: []string{"accounts.events"},
	})
	if err != nil {
		t.Fatalf("construct failing codec: %v", err)
	}
	delivery, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	_, recordError := codec.Encode(delivery)

	workingCodec := testRecordCodec(t)
	dispatcher, err := NewDispatcher(
		publisherFunc(func(context.Context, kafka.Message) error {
			return sensitive
		}),
		workingCodec,
	)
	if err != nil {
		t.Fatalf("construct failing dispatcher: %v", err)
	}
	dispatchError := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{delivery},
	)

	record := consumedRecord(encodedLiveRecord(t, workingCodec, testMessage(t)))
	handler, err := NewRecordHandler(
		workingCodec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return sensitive
		}),
	)
	if err != nil {
		t.Fatalf("construct failing handler: %v", err)
	}
	handlerError := handler.Handle(context.Background(), record)

	policy, err := NewDeadLetterPolicy(
		publisherFunc(func(context.Context, kafka.Message) error {
			return sensitive
		}),
		DeadLetterPolicyConfig{Topic: "accounts.events.dead-letter"},
	)
	if err != nil {
		t.Fatalf("construct failing dead-letter policy: %v", err)
	}
	_, deadLetterError := policy.HandleFailure(
		context.Background(),
		record,
		errors.New("application failure"),
	)

	for name, candidate := range map[string]error{
		"record":      recordError,
		"dispatch":    dispatchError,
		"handler":     handlerError,
		"dead letter": deadLetterError,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !errors.Is(candidate, sensitive) {
				t.Fatal("retained cause is unavailable through errors.Is")
			}
			for _, diagnostic := range []string{
				fmt.Sprintf("%s", candidate),
				fmt.Sprintf("%q", candidate),
				fmt.Sprintf("%v", candidate),
				fmt.Sprintf("%+v", candidate),
				fmt.Sprintf("%#v", candidate),
			} {
				if strings.Contains(diagnostic, "formatter-secret") {
					t.Fatalf("sensitive formatter output = %q", diagnostic)
				}
			}
		})
	}
}

type publisherFunc func(context.Context, kafka.Message) error

func (publisher publisherFunc) Publish(
	ctx context.Context,
	message kafka.Message,
) error {
	return publisher(ctx, message)
}
