package gotelemetry

import (
	"context"
	"sync"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestAllWrappersRaceCallerOwnedSDKShutdown(t *testing.T) {
	tracerProvider := sdktrace.NewTracerProvider()
	meterProvider := sdkmetric.NewMeterProvider()
	instrumentation, err := New(testRuntime{
		tracer:     tracerProvider,
		meter:      meterProvider,
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	delivery := telemetryDelivery(t, "concurrent", eventsourcing.DeliveryReplay)
	decoded, encoded := telemetrySerializationEvents(t)
	upcastEvent, err := eventsourcing.NewUpcastEvent(encoded, map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("NewUpcastEvent() error = %v", err)
	}

	dispatcher := mustWrapDispatcher(t, instrumentation)
	consumer := mustWrapConsumer(t, instrumentation)
	eventStore := mustWrapConcurrentEventStore(t, instrumentation)
	globalReader := mustWrapConcurrentGlobalReader(t, instrumentation)
	snapshotStore := mustWrapConcurrentSnapshotStore(t, instrumentation)
	runner := mustWrapConcurrentProjectionRunner(t, instrumentation)
	checkpointStore := mustWrapConcurrentCheckpointStore(t, instrumentation)
	controller := mustWrapConcurrentProjectionController(t, instrumentation)
	handler := mustWrapConcurrentProjectionHandler(t, instrumentation)
	manager := mustWrapConcurrentProcessManager(t, instrumentation)
	codec := mustWrapConcurrentCodec(t, instrumentation, decoded, encoded)
	upcaster := mustWrapConcurrentUpcaster(t, instrumentation)
	publisher := mustWrapConcurrentKafkaPublisher(t, instrumentation)
	kafkaHandler := mustWrapConcurrentKafkaHandler(t, instrumentation)

	const workers = 128
	start := make(chan struct{})
	errorsFound := make(chan error, workers*32)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			ctx := context.Background()
			recordConcurrentError(errorsFound, dispatcher.Dispatch(ctx, []eventsourcing.Delivery{delivery}))
			recordConcurrentError(errorsFound, consumer(ctx, delivery))
			_, appendErr := eventStore.Append(ctx, eventsourcing.StreamID{}, eventsourcing.ExpectedVersion{}, nil)
			recordConcurrentError(errorsFound, appendErr)
			streamIterator, readErr := eventStore.ReadStream(ctx, eventsourcing.StreamID{}, eventsourcing.ReadStreamOptions{})
			recordConcurrentError(errorsFound, readErr)
			if streamIterator != nil {
				recordConcurrentError(errorsFound, streamIterator.Close())
			}
			globalIterator, readErr := globalReader.ReadGlobal(ctx, eventsourcing.ReadGlobalOptions{})
			recordConcurrentError(errorsFound, readErr)
			if globalIterator != nil {
				recordConcurrentError(errorsFound, globalIterator.Close())
			}
			_, loadErr := snapshotStore.Load(ctx, eventsourcing.StreamID{})
			recordConcurrentError(errorsFound, loadErr)
			recordConcurrentError(errorsFound, snapshotStore.Save(ctx, eventsourcing.Snapshot{}))
			recordConcurrentError(errorsFound, snapshotStore.Delete(ctx, eventsourcing.StreamID{}))
			_, runErr := runner.RunBatch(ctx)
			recordConcurrentError(errorsFound, runErr)
			_, statusErr := checkpointStore.Status(ctx, "projection")
			recordConcurrentError(errorsFound, statusErr)
			recordConcurrentError(errorsFound, checkpointStore.Save(ctx, "projection", 0, 1))
			_, controlErr := controller.Status(ctx)
			recordConcurrentError(errorsFound, controlErr)
			_, controlErr = controller.Pause(ctx)
			recordConcurrentError(errorsFound, controlErr)
			_, controlErr = controller.Resume(ctx)
			recordConcurrentError(errorsFound, controlErr)
			_, controlErr = controller.ResetCheckpoint(ctx, 0)
			recordConcurrentError(errorsFound, controlErr)
			recordConcurrentError(errorsFound, handler(ctx, delivery))
			_, planErr := manager.Plan(ctx, delivery)
			recordConcurrentError(errorsFound, planErr)
			_, encodeErr := codec.EncodeContext(ctx, decoded)
			recordConcurrentError(errorsFound, encodeErr)
			_, decodeErr := codec.DecodeContext(ctx, encoded)
			recordConcurrentError(errorsFound, decodeErr)
			_, upcastErr := upcaster.UpcastContext(ctx, upcastEvent)
			recordConcurrentError(errorsFound, upcastErr)
			recordConcurrentError(errorsFound, publisher.Publish(ctx, kafka.Message{}))
			recordConcurrentError(errorsFound, kafkaHandler.Handle(ctx, kafka.ConsumedMessage{}))
			recordConcurrentError(errorsFound, instrumentation.RecordProjectionLag(ctx, "projection", 0, 1))
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		<-start
		recordConcurrentError(errorsFound, tracerProvider.Shutdown(context.Background()))
		recordConcurrentError(errorsFound, meterProvider.Shutdown(context.Background()))
	}()
	close(start)
	group.Wait()
	close(errorsFound)
	for operationErr := range errorsFound {
		if operationErr != nil {
			t.Fatalf("concurrent wrapper error = %v", operationErr)
		}
	}
}

func recordConcurrentError(destination chan<- error, err error) {
	if err != nil {
		destination <- err
	}
}

func mustWrapDispatcher(t testing.TB, instrumentation *Instrumentation) eventsourcing.Dispatcher {
	t.Helper()
	wrapper, err := instrumentation.WrapDispatcher(dispatcherFunc(func(context.Context, []eventsourcing.Delivery) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func mustWrapConsumer(t testing.TB, instrumentation *Instrumentation) eventsourcing.ConsumerFunc {
	t.Helper()
	wrapper, err := instrumentation.WrapConsumer(func(context.Context, eventsourcing.Delivery) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentStore struct{}

func (concurrentStore) Append(context.Context, eventsourcing.StreamID, eventsourcing.ExpectedVersion, []eventsourcing.PendingMessage) ([]eventsourcing.Message, error) {
	return nil, nil
}
func (concurrentStore) ReadStream(context.Context, eventsourcing.StreamID, eventsourcing.ReadStreamOptions) (eventsourcing.MessageIterator, error) {
	return &telemetryIterator{}, nil
}

func mustWrapConcurrentEventStore(t testing.TB, instrumentation *Instrumentation) eventsourcing.EventStore {
	t.Helper()
	wrapper, err := instrumentation.WrapEventStore(concurrentStore{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentGlobalReader struct{}

func (concurrentGlobalReader) ReadGlobal(context.Context, eventsourcing.ReadGlobalOptions) (eventsourcing.MessageIterator, error) {
	return &telemetryIterator{}, nil
}

func mustWrapConcurrentGlobalReader(t testing.TB, instrumentation *Instrumentation) eventsourcing.GlobalReader {
	t.Helper()
	wrapper, err := instrumentation.WrapGlobalReader(concurrentGlobalReader{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentSnapshotStore struct{}

func (concurrentSnapshotStore) Load(context.Context, eventsourcing.StreamID) (eventsourcing.Snapshot, error) {
	return eventsourcing.Snapshot{}, nil
}
func (concurrentSnapshotStore) Save(context.Context, eventsourcing.Snapshot) error   { return nil }
func (concurrentSnapshotStore) Delete(context.Context, eventsourcing.StreamID) error { return nil }

func mustWrapConcurrentSnapshotStore(t testing.TB, instrumentation *Instrumentation) eventsourcing.SnapshotStore {
	t.Helper()
	wrapper, err := instrumentation.WrapSnapshotStore(concurrentSnapshotStore{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentProjectionRunner struct{}

func (concurrentProjectionRunner) RunBatch(context.Context) (projection.BatchResult, error) {
	return projection.BatchResult{}, nil
}

func mustWrapConcurrentProjectionRunner(t testing.TB, instrumentation *Instrumentation) ProjectionRunner {
	t.Helper()
	wrapper, err := instrumentation.WrapProjectionRunner("projection", concurrentProjectionRunner{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentCheckpointStore struct{}

func (concurrentCheckpointStore) Status(context.Context, string) (projection.Status, error) {
	return projection.Status{}, nil
}
func (concurrentCheckpointStore) Save(context.Context, string, eventsourcing.GlobalPosition, eventsourcing.GlobalPosition) error {
	return nil
}

func mustWrapConcurrentCheckpointStore(t testing.TB, instrumentation *Instrumentation) projection.CheckpointStore {
	t.Helper()
	wrapper, err := instrumentation.WrapProjectionCheckpointStore(concurrentCheckpointStore{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentProjectionController struct{}

func (concurrentProjectionController) Status(context.Context) (projection.Status, error) {
	return projection.Status{}, nil
}
func (concurrentProjectionController) Pause(context.Context) (projection.Status, error) {
	return projection.Status{}, nil
}
func (concurrentProjectionController) Resume(context.Context) (projection.Status, error) {
	return projection.Status{}, nil
}
func (concurrentProjectionController) ResetCheckpoint(context.Context, eventsourcing.GlobalPosition) (projection.Status, error) {
	return projection.Status{}, nil
}

func mustWrapConcurrentProjectionController(t testing.TB, instrumentation *Instrumentation) ProjectionController {
	t.Helper()
	wrapper, err := instrumentation.WrapProjectionController("projection", concurrentProjectionController{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func mustWrapConcurrentProjectionHandler(t testing.TB, instrumentation *Instrumentation) projection.Handler {
	t.Helper()
	wrapper, err := instrumentation.WrapProjectionHandler("projection", func(context.Context, eventsourcing.Delivery) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentProcessManager struct{}

func (concurrentProcessManager) Plan(context.Context, eventsourcing.Delivery) (processmanager.PlanResult[uint64], error) {
	return processmanager.PlanResult[uint64]{}, nil
}

func mustWrapConcurrentProcessManager(t testing.TB, instrumentation *Instrumentation) ProcessManager[uint64] {
	t.Helper()
	wrapper, err := WrapProcessManager(instrumentation, "manager", concurrentProcessManager{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentCodec struct {
	decoded eventsourcing.DecodedEvent
	encoded eventsourcing.EncodedEvent
}

func (codec concurrentCodec) Encode(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
	return codec.encoded, nil
}
func (codec concurrentCodec) Decode(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error) {
	return codec.decoded, nil
}

func mustWrapConcurrentCodec(t testing.TB, instrumentation *Instrumentation, decoded eventsourcing.DecodedEvent, encoded eventsourcing.EncodedEvent) eventsourcing.ContextPayloadCodec {
	t.Helper()
	wrapper, err := instrumentation.WrapPayloadCodec(concurrentCodec{decoded: decoded, encoded: encoded})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentUpcaster struct{}

func (concurrentUpcaster) Upcast(event eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
	return []eventsourcing.UpcastEvent{event}, nil
}

func mustWrapConcurrentUpcaster(t testing.TB, instrumentation *Instrumentation) eventsourcing.ContextUpcaster {
	t.Helper()
	wrapper, err := instrumentation.WrapUpcaster(concurrentUpcaster{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

type concurrentKafkaPublisher struct{}

func (concurrentKafkaPublisher) Publish(context.Context, kafka.Message) error { return nil }

func mustWrapConcurrentKafkaPublisher(t testing.TB, instrumentation *Instrumentation) KafkaPublisher {
	t.Helper()
	wrapper, err := instrumentation.WrapKafkaPublisher(concurrentKafkaPublisher{}, KafkaPropagationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func mustWrapConcurrentKafkaHandler(t testing.TB, instrumentation *Instrumentation) kafka.Handler {
	t.Helper()
	wrapper, err := instrumentation.WrapKafkaHandler(kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error { return nil }), KafkaPropagationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}
