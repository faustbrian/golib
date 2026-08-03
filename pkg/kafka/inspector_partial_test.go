package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

func TestInspectorReturnsTypedPerTargetTopicResults(t *testing.T) {
	t.Parallel()

	backend := newPartialInspectorBackend()
	backend.topics["healthy"] = partialInspectionTopic("healthy")
	backend.topicErrors["denied"] = kerr.TopicAuthorizationFailed
	inspector := &Inspector{
		admin:                    backend,
		client:                   backend,
		requestTimeout:           time.Second,
		maxConcurrentInspections: 2,
	}

	results, err := inspector.InspectTopics(
		context.Background(),
		"denied",
		"healthy",
	)
	if !errors.Is(err, ErrInspectionTargetsFailed) {
		t.Fatalf("InspectTopics() error = %v", err)
	}
	if len(results) != 2 ||
		results[0].Topic != "denied" ||
		results[0].State.Name != "" ||
		len(results[0].State.Partitions) != 0 ||
		results[0].Category != ErrorAuthorization ||
		!errors.Is(results[0].Err, kerr.TopicAuthorizationFailed) ||
		results[1].Topic != "healthy" ||
		results[1].State.Name != "healthy" ||
		results[1].Category != ErrorUnknown ||
		results[1].Err != nil {
		t.Fatalf("InspectTopics() = %#v", results)
	}
}

func TestInspectorReturnsTypedPerTargetGroupResults(t *testing.T) {
	t.Parallel()

	backend := newPartialInspectorBackend()
	backend.groups["healthy"] = inspectorGroupLag{
		group: "healthy",
		state: "Stable",
		lag:   kadm.GroupLag{},
	}
	backend.groupErrors["missing"] = kerr.GroupIDNotFound
	inspector := &Inspector{
		admin:                    backend,
		client:                   backend,
		requestTimeout:           time.Second,
		maxConcurrentInspections: 2,
	}

	results, err := inspector.InspectConsumerGroups(
		context.Background(),
		"healthy",
		"missing",
	)
	if !errors.Is(err, ErrInspectionTargetsFailed) {
		t.Fatalf("InspectConsumerGroups() error = %v", err)
	}
	if len(results) != 2 ||
		results[0].Group != "healthy" ||
		results[0].State.Group != "healthy" ||
		results[0].Category != ErrorUnknown ||
		results[0].Err != nil ||
		results[1].Group != "missing" ||
		results[1].State.Group != "" ||
		len(results[1].State.Partitions) != 0 ||
		results[1].Category != ErrorPermanent ||
		!errors.Is(results[1].Err, kerr.GroupIDNotFound) {
		t.Fatalf("InspectConsumerGroups() = %#v", results)
	}
}

func TestInspectorBoundsConcurrentPerTargetRequests(t *testing.T) {
	t.Parallel()

	backend := newPartialInspectorBackend()
	backend.block = make(chan struct{})
	backend.started = make(chan struct{}, 3)
	for _, topic := range []string{"one", "two", "three"} {
		backend.topics[topic] = partialInspectionTopic(topic)
	}
	inspector := &Inspector{
		admin:                    backend,
		client:                   backend,
		requestTimeout:           time.Second,
		maxConcurrentInspections: 2,
	}

	type inspectionResult struct {
		results []TopicInspectionResult
		err     error
	}
	done := make(chan inspectionResult, 1)
	go func() {
		results, err := inspector.InspectTopics(
			context.Background(),
			"one",
			"two",
			"three",
		)
		done <- inspectionResult{results: results, err: err}
	}()
	for range 2 {
		select {
		case <-backend.started:
		case result := <-done:
			t.Fatalf(
				"InspectTopics() returned before starting bounded workers: %#v, %v",
				result.results,
				result.err,
			)
		}
	}
	select {
	case <-backend.started:
		t.Fatal("third inspection started before bounded workers were released")
	default:
	}
	if got := backend.maximumActive(); got != 2 {
		t.Fatalf("maximum concurrent inspections = %d", got)
	}
	close(backend.block)

	result := <-done
	if result.err != nil || len(result.results) != 3 {
		t.Fatalf("InspectTopics() = %#v, %v", result.results, result.err)
	}
}

func TestInspectorPartialMethodsValidateBeforeRequests(t *testing.T) {
	t.Parallel()

	backend := newPartialInspectorBackend()
	inspector := &Inspector{admin: backend, client: backend}
	if _, err := inspector.InspectTopics(
		context.Background(),
		"duplicate",
		"duplicate",
	); !errors.Is(err, ErrDuplicateInspectionTarget) {
		t.Fatalf("InspectTopics() error = %v", err)
	}
	if _, err := inspector.InspectConsumerGroups(
		context.Background(),
	); !errors.Is(err, ErrInspectionTargetsRequired) {
		t.Fatalf("InspectConsumerGroups() error = %v", err)
	}
	if got := backend.callCount(); got != 0 {
		t.Fatalf("backend calls = %d", got)
	}
}

func TestInspectorPartialMethodsPreserveLifecycleAndCancellation(t *testing.T) {
	t.Parallel()

	backend := newPartialInspectorBackend()
	backend.topics["healthy"] = partialInspectionTopic("healthy")
	backend.groups["healthy"] = inspectorGroupLag{
		group: "healthy",
		state: "Stable",
		lag:   kadm.GroupLag{},
	}
	inspector := &Inspector{
		admin:          backend,
		client:         backend,
		requestTimeout: time.Second,
	}
	var nilContext context.Context
	if _, err := inspector.InspectTopics(nilContext, "healthy"); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("InspectTopics(nil) error = %v", err)
	}
	if _, err := inspector.InspectConsumerGroups(nilContext, "healthy"); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("InspectConsumerGroups(nil) error = %v", err)
	}
	results, err := inspector.InspectTopics(context.Background(), "healthy")
	if err != nil || len(results) != 1 || results[0].State.Name != "healthy" {
		t.Fatalf("InspectTopics() = %#v, %v", results, err)
	}
	groups, err := inspector.InspectConsumerGroups(
		context.Background(),
		"healthy",
	)
	if err != nil || len(groups) != 1 || groups[0].State.Group != "healthy" {
		t.Fatalf("InspectConsumerGroups() = %#v, %v", groups, err)
	}
	if err := inspector.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := inspector.InspectTopics(
		context.Background(),
		"healthy",
	); !errors.Is(err, ErrInspectorClosed) {
		t.Fatalf("InspectTopics() after close error = %v", err)
	}

	cancelBackend := newPartialInspectorBackend()
	cancelBackend.block = make(chan struct{})
	for _, group := range []string{"one", "two", "three"} {
		cancelBackend.groups[group] = inspectorGroupLag{
			group: group,
			state: "Stable",
			lag:   kadm.GroupLag{},
		}
	}
	cancelInspector := &Inspector{
		admin:                    cancelBackend,
		client:                   cancelBackend,
		requestTimeout:           time.Second,
		maxConcurrentInspections: 2,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	groupResults, err := cancelInspector.InspectConsumerGroups(
		ctx,
		"one",
		"two",
		"three",
	)
	if !errors.Is(err, ErrInspectionTargetsFailed) || len(groupResults) != 3 {
		t.Fatalf("canceled InspectConsumerGroups() = %#v, %v", groupResults, err)
	}
	for index, result := range groupResults {
		if !errors.Is(result.Err, context.Canceled) ||
			result.Category != ErrorCanceled {
			t.Fatalf("canceled group result %d = %#v", index, result)
		}
	}
}

func TestInspectorConfigBoundsConcurrentInspections(t *testing.T) {
	t.Parallel()

	config, err := normalizeInspectorConfig(InspectorConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "inspector",
	})
	if err != nil || config.MaxConcurrentInspections != 4 {
		t.Fatalf("default concurrent inspections = %d, %v", config.MaxConcurrentInspections, err)
	}
	for _, maximum := range []int{1, 64} {
		config.MaxConcurrentInspections = maximum
		if _, err := normalizeInspectorConfig(config); err != nil {
			t.Fatalf("concurrent inspection boundary %d error = %v", maximum, err)
		}
	}
	for _, maximum := range []int{-1, 65} {
		config.MaxConcurrentInspections = maximum
		if _, err := normalizeInspectorConfig(config); !errors.Is(
			err,
			ErrInvalidInspectorConfig,
		) {
			t.Fatalf("concurrent inspection limit %d error = %v", maximum, err)
		}
	}
}

func partialInspectionTopic(topic string) kadm.TopicDetail {
	return kadm.TopicDetail{
		Topic: topic,
		Partitions: kadm.PartitionDetails{
			0: {
				Topic: topic, Partition: 0, Leader: 1,
				Replicas: []int32{1}, ISR: []int32{1},
			},
		},
	}
}

type partialInspectorBackend struct {
	mu          sync.Mutex
	topics      map[string]kadm.TopicDetail
	topicErrors map[string]error
	groups      map[string]inspectorGroupLag
	groupErrors map[string]error
	block       chan struct{}
	started     chan struct{}
	active      int
	maximum     int
	calls       int
}

func newPartialInspectorBackend() *partialInspectorBackend {
	return &partialInspectorBackend{
		topics:      make(map[string]kadm.TopicDetail),
		topicErrors: make(map[string]error),
		groups:      make(map[string]inspectorGroupLag),
		groupErrors: make(map[string]error),
	}
}

func (backend *partialInspectorBackend) Lag(
	ctx context.Context,
	groups ...string,
) (inspectorGroupLags, error) {
	if len(groups) != 1 {
		return nil, ErrInvalidInspectionResponse
	}
	backend.begin(ctx)
	defer backend.end()
	if err := backend.groupErrors[groups[0]]; err != nil {
		return nil, err
	}
	group, exists := backend.groups[groups[0]]
	if !exists {
		return nil, kerr.GroupIDNotFound
	}

	return inspectorGroupLags{groups[0]: group}, nil
}

func (backend *partialInspectorBackend) Metadata(
	ctx context.Context,
	topics ...string,
) (kadm.Metadata, error) {
	if len(topics) != 1 {
		return kadm.Metadata{}, ErrInvalidInspectionResponse
	}
	backend.begin(ctx)
	defer backend.end()
	if err := backend.topicErrors[topics[0]]; err != nil {
		return kadm.Metadata{}, err
	}
	topic, exists := backend.topics[topics[0]]
	if !exists {
		return kadm.Metadata{}, kerr.UnknownTopicOrPartition
	}

	return kadm.Metadata{Topics: kadm.TopicDetails{topics[0]: topic}}, nil
}

func (backend *partialInspectorBackend) BrokerMetadata(
	context.Context,
) (kadm.Metadata, error) {
	return kadm.Metadata{}, nil
}

func (backend *partialInspectorBackend) ListStartOffsets(
	_ context.Context,
	topics ...string,
) (kadm.ListedOffsets, error) {
	return backend.offsets(topics), nil
}

func (backend *partialInspectorBackend) ListEndOffsets(
	_ context.Context,
	topics ...string,
) (kadm.ListedOffsets, error) {
	return backend.offsets(topics), nil
}

func (backend *partialInspectorBackend) ListPartitionOffsets(
	context.Context,
	int64,
	[]TopicPartition,
) (kadm.ListedOffsets, error) {
	return nil, nil
}

func (backend *partialInspectorBackend) DescribeTopicConfigs(
	_ context.Context,
	topics ...string,
) (kadm.ResourceConfigs, error) {
	return kadm.ResourceConfigs{
		validTopicInspectionResource(topics[0], "1"),
	}, nil
}

func (backend *partialInspectorBackend) Ping(context.Context) error {
	return nil
}

func (backend *partialInspectorBackend) Close() {}

func (backend *partialInspectorBackend) offsets(
	topics []string,
) kadm.ListedOffsets {
	topic := backend.topics[topics[0]]

	return inspectorOffsetsForTopics(kadm.TopicDetails{topic.Topic: topic})
}

func (backend *partialInspectorBackend) begin(ctx context.Context) {
	backend.mu.Lock()
	backend.calls++
	backend.active++
	if backend.active > backend.maximum {
		backend.maximum = backend.active
	}
	started := backend.started
	block := backend.block
	backend.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
}

func (backend *partialInspectorBackend) end() {
	backend.mu.Lock()
	backend.active--
	backend.mu.Unlock()
}

func (backend *partialInspectorBackend) maximumActive() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	return backend.maximum
}

func (backend *partialInspectorBackend) callCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	return backend.calls
}
