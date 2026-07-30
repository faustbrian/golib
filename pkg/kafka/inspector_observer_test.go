package kafka

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestInspectorObserversReportInspectionHealthReadinessAndShutdown(
	t *testing.T,
) {

	backend := &metadataInspectorBackend{
		brokerMetadata: kadm.Metadata{
			Cluster:    "cluster-1",
			Controller: 1,
			Brokers: kadm.BrokerDetails{{
				NodeID: 1, Host: "broker.internal", Port: 9092,
			}},
		},
		metadata: kadm.Metadata{Topics: kadm.TopicDetails{
			"events": {
				Topic: "events",
				Partitions: kadm.PartitionDetails{0: {
					Topic: "events", Partition: 0, Leader: 1,
					Replicas: []int32{1}, ISR: []int32{1},
				}},
			},
		}},
		startOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0}},
		},
		endOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0, Offset: 10}},
		},
		configs: kadm.ResourceConfigs{
			validTopicInspectionResource("events", "1"),
		},
		recordingInspectorBackend: recordingInspectorBackend{
			groupLags: inspectorGroupLags{
				"workers": {
					group:         "workers",
					coordinatorID: 1,
					state:         "Stable",
					protocolType:  "consumer",
					protocol:      "cooperative-sticky",
					members: []inspectorGroupMember{{
						memberID:          "member-1",
						clientID:          "worker",
						clientHost:        "/127.0.0.1",
						assignmentDecoded: true,
						assignments: map[string][]int32{
							"events": {0},
						},
					}},
					lag: kadm.GroupLag{
						"events": {0: {
							Topic: "events", Partition: 0,
							Commit: kadm.Offset{
								Topic: "events", Partition: 0, At: 5,
							},
							Start: kadm.ListedOffset{
								Topic: "events", Partition: 0,
							},
							End: kadm.ListedOffset{
								Topic: "events", Partition: 0, Offset: 10,
							},
							Lag: 5,
						}},
					},
				},
			},
		},
	}
	var observations []Observation
	inspector := inspectorWithMetadataBackend(backend)
	inspector.clientID = "operations-inspector"
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  1,
		RecoveryThreshold: 1,
	}
	inspector.observers = newObserverDispatcher(mustNormalizeObserverPolicy(
		t,
		ObserverPolicy{
			Observers: []ObserverFunc{
				func(_ context.Context, observation Observation) error {
					observations = append(observations, observation)

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	))

	if _, err := inspector.Cluster(context.Background()); err != nil {
		t.Fatalf("Cluster() error = %v", err)
	}
	if _, err := inspector.Topics(context.Background(), "events"); err != nil {
		t.Fatalf("Topics() error = %v", err)
	}
	if _, err := inspector.ConsumerGroupLag(
		context.Background(),
		"workers",
	); err != nil {
		t.Fatalf("ConsumerGroupLag() error = %v", err)
	}
	if err := inspector.DependencyHealth(context.Background()); err != nil {
		t.Fatalf("DependencyHealth() error = %v", err)
	}
	state, err := inspector.Readiness(context.Background())
	if err != nil || !state.Ready || !state.DependencyHealthy {
		t.Fatalf("Readiness() = %#v, %v", state, err)
	}
	if err := inspector.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := inspector.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	if len(observations) != 7 {
		t.Fatalf("observation count = %d, want 7: %#v", len(observations), observations)
	}
	for _, observation := range observations {
		if observation.ClientID != "operations-inspector" ||
			observation.StartedAt.IsZero() ||
			observation.Duration < 0 ||
			observation.Category != ErrorUnknown ||
			!observation.Succeeded {
			t.Fatalf("common inspector observation = %#v", observation)
		}
		if err := observation.Validate(); err != nil {
			t.Fatalf("Observation.Validate() error = %v for %#v", err, observation)
		}
	}
	want := []ObservationKind{
		ObservationInspectorCluster,
		ObservationInspectorTopics,
		ObservationInspectorConsumerGroups,
		ObservationDependencyHealth,
		ObservationDependencyHealth,
		ObservationReadiness,
		ObservationInspectorShutdown,
	}
	got := make([]ObservationKind, 0, len(observations))
	for _, observation := range observations {
		got = append(got, observation.Kind)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("observation kinds = %v, want %v", got, want)
	}
	if observations[0].BrokerCount != 1 ||
		observations[1].TopicCount != 1 ||
		observations[1].PartitionCount != 1 ||
		observations[2].GroupCount != 1 ||
		observations[2].GroupMemberCount != 1 ||
		observations[2].PartitionCount != 1 {
		t.Fatalf("inspection counts = %#v", observations[:3])
	}
	if !observations[3].DependencyHealthy ||
		!observations[4].DependencyHealthy ||
		!observations[5].DependencyHealthy ||
		!observations[5].Ready ||
		observations[5].ConsecutiveFailures != 0 ||
		observations[5].ConsecutiveSuccesses != 1 {
		t.Fatalf("health/readiness observations = %#v", observations[3:6])
	}
}

func TestInspectorObserversReportBoundedFailures(t *testing.T) {

	failure := errors.New("inspection unavailable")
	backend := &metadataInspectorBackend{
		brokerMetadataErr: failure,
		metadataErr:       failure,
		recordingInspectorBackend: recordingInspectorBackend{
			lagErr:    failure,
			healthErr: failure,
		},
	}
	var observations []Observation
	inspector := inspectorWithMetadataBackend(backend)
	inspector.clientID = "failing-inspector"
	inspector.observers = newObserverDispatcher(mustNormalizeObserverPolicy(
		t,
		ObserverPolicy{
			Observers: []ObserverFunc{
				func(_ context.Context, observation Observation) error {
					observations = append(observations, observation)

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	))

	_, _ = inspector.Cluster(context.Background())
	_, _ = inspector.Topics(context.Background(), "events")
	_, _ = inspector.ConsumerGroupLag(context.Background(), "workers")
	_ = inspector.DependencyHealth(context.Background())

	if len(observations) != 4 {
		t.Fatalf("observation count = %d, want 4", len(observations))
	}
	wantKinds := []ObservationKind{
		ObservationInspectorCluster,
		ObservationInspectorTopics,
		ObservationInspectorConsumerGroups,
		ObservationDependencyHealth,
	}
	for index, observation := range observations {
		if observation.Kind != wantKinds[index] ||
			observation.Succeeded ||
			observation.Category != ErrorPermanent {
			t.Fatalf("failed observation %d = %#v", index, observation)
		}
	}
	if observations[0].BrokerCount != 0 ||
		observations[1].TopicCount != 1 ||
		observations[1].PartitionCount != 0 ||
		observations[2].GroupCount != 1 ||
		observations[2].GroupMemberCount != 0 ||
		observations[2].PartitionCount != 0 ||
		observations[3].DependencyHealthy {
		t.Fatalf("failed observation counts = %#v", observations)
	}
}

func TestInspectorReadinessObserversReportHysteresisAndSkipInconclusiveCalls(
	t *testing.T,
) {

	dependencyErr := errors.New("dependency unavailable")
	outcomes := []error{nil, dependencyErr}
	backend := &metadataInspectorBackend{}
	backend.healthFn = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		outcome := outcomes[0]
		outcomes = outcomes[1:]

		return outcome
	}
	var observations []Observation
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  2,
		RecoveryThreshold: 2,
	}
	inspector.observers = newObserverDispatcher(mustNormalizeObserverPolicy(
		t,
		ObserverPolicy{
			Observers: []ObserverFunc{
				func(_ context.Context, observation Observation) error {
					observations = append(observations, observation)

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	))

	state, err := inspector.Readiness(context.Background())
	if err != nil || state.Ready || !state.DependencyHealthy ||
		state.ConsecutiveSuccesses != 1 {
		t.Fatalf("successful Readiness() = %#v, %v", state, err)
	}
	state, err = inspector.Readiness(context.Background())
	if !errors.Is(err, dependencyErr) ||
		state.Ready ||
		state.DependencyHealthy ||
		state.ConsecutiveFailures != 1 {
		t.Fatalf("failed Readiness() = %#v, %v", state, err)
	}
	beforeInconclusive := len(observations)
	//nolint:staticcheck // Nil probes are an explicit inconclusive contract.
	//lint:ignore SA1012 Nil probes are an explicit inconclusive contract.
	state, err = inspector.Readiness(nil)
	if !errors.Is(err, ErrContextRequired) ||
		state.DependencyHealthy ||
		state.ConsecutiveFailures != 1 ||
		len(observations) != beforeInconclusive {
		t.Fatalf("Readiness(nil) = %#v, %v, observations=%d", state, err, len(observations))
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state, err = inspector.Readiness(canceled)
	if !errors.Is(err, context.Canceled) ||
		state.DependencyHealthy ||
		state.ConsecutiveFailures != 1 ||
		len(observations) != beforeInconclusive+1 {
		t.Fatalf(
			"Readiness(canceled) = %#v, %v, observations=%d",
			state,
			err,
			len(observations),
		)
	}

	if len(observations) != 5 {
		t.Fatalf("observations = %#v", observations)
	}
	if observations[0].Kind != ObservationDependencyHealth ||
		observations[1].Kind != ObservationReadiness ||
		!observations[0].Succeeded ||
		!observations[1].Succeeded ||
		!observations[1].DependencyHealthy ||
		observations[1].Ready ||
		observations[1].ConsecutiveSuccesses != 1 {
		t.Fatalf("successful readiness observations = %#v", observations[:2])
	}
	if observations[2].Kind != ObservationDependencyHealth ||
		observations[3].Kind != ObservationReadiness ||
		observations[2].Succeeded ||
		observations[3].Succeeded ||
		observations[3].DependencyHealthy ||
		observations[3].Ready ||
		observations[3].ConsecutiveFailures != 1 {
		t.Fatalf("failed readiness observations = %#v", observations[2:4])
	}
	if observations[4].Kind != ObservationDependencyHealth ||
		observations[4].Category != ErrorCanceled {
		t.Fatalf("canceled dependency observation = %#v", observations[4])
	}
}

func TestInspectorObserversFenceSameInspectorReentry(t *testing.T) {

	backend := &metadataInspectorBackend{
		brokerMetadata: kadm.Metadata{
			Cluster:    "cluster-1",
			Controller: 1,
			Brokers: kadm.BrokerDetails{{
				NodeID: 1, Host: "broker.internal", Port: 9092,
			}},
		},
	}
	inspector := inspectorWithMetadataBackend(backend)
	reentry := make(chan []error, 1)
	inspector.observers = newObserverDispatcher(mustNormalizeObserverPolicy(
		t,
		ObserverPolicy{
			Observers: []ObserverFunc{
				func(ctx context.Context, _ Observation) error {
					_, clusterErr := inspector.Cluster(ctx)
					_, topicErr := inspector.Topics(context.Background(), "events")
					_, groupErr := inspector.ConsumerGroupLag(
						context.Background(),
						"workers",
					)
					healthErr := inspector.DependencyHealth(context.Background())
					_, readinessContextErr := inspector.Readiness(ctx)
					_, readinessErr := inspector.Readiness(context.Background())
					closeErr := inspector.Close()
					reentry <- []error{
						clusterErr,
						topicErr,
						groupErr,
						healthErr,
						readinessContextErr,
						readinessErr,
						closeErr,
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	))

	if _, err := inspector.Cluster(context.Background()); err != nil {
		t.Fatalf("Cluster() error = %v", err)
	}
	for index, err := range <-reentry {
		if !errors.Is(err, ErrObserverReentry) {
			t.Fatalf("reentry error %d = %v, want %v", index, err, ErrObserverReentry)
		}
	}
	if inspector.closed.Load() || backend.closed != 0 {
		t.Fatalf("observer reentry closed inspector: closed=%v calls=%d", inspector.closed.Load(), backend.closed)
	}
	if err := inspector.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestInspectorConfigValidatesCopiesAndWiresObservers(t *testing.T) {

	observer := ObserverFunc(func(context.Context, Observation) error {
		return nil
	})
	policy := ObserverPolicy{
		Observers:      []ObserverFunc{observer},
		FailureHandler: func(context.Context, ObservationFailure) {},
	}
	config, err := normalizeInspectorConfig(InspectorConfig{
		Brokers:   []string{"broker.internal:9092"},
		ClientID:  "operations-inspector",
		Observers: policy,
	})
	if err != nil {
		t.Fatalf("normalizeInspectorConfig() error = %v", err)
	}
	policy.Observers[0] = nil
	if reflect.ValueOf(config.Observers.Observers[0]).IsNil() {
		t.Fatal("normalizeInspectorConfig() retained observer slice")
	}

	factoryCalled := false
	_, err = newInspector(
		InspectorConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "operations-inspector",
			Observers: ObserverPolicy{
				Observers: []ObserverFunc{observer},
			},
		},
		func(...kgo.Opt) (*kgo.Client, error) {
			factoryCalled = true

			return nil, errors.New("unexpected client construction")
		},
		func(*kgo.Client, InspectorConfig) inspectorBackend {
			t.Fatal("admin factory called for invalid observer configuration")

			return nil
		},
	)
	if !errors.Is(err, ErrObserverFailureHandlerRequired) {
		t.Fatalf("newInspector() error = %v", err)
	}
	if factoryCalled {
		t.Fatal("newInspector() allocated a client before observer validation")
	}
}

func TestInspectorWiresBrokerObserversAndFencesLifecycleReentry(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-serverDone
	})

	observed := make(chan Observation, 1)
	reentryResult := make(chan error, 1)
	var firstConnection sync.Once
	var inspector *Inspector
	inspector, err = NewInspector(InspectorConfig{
		Brokers:     []string{listener.Addr().String()},
		ClientID:    "wired-inspector",
		DialTimeout: 100 * time.Millisecond,
		Security:    DevelopmentPlaintextSecurity(),
		Observers: ObserverPolicy{
			Observers: []ObserverFunc{
				func(_ context.Context, observation Observation) error {
					if observation.Kind == ObservationBrokerConnect {
						firstConnection.Do(func() {
							reentryResult <- inspector.Close()
							observed <- observation
						})
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("NewInspector() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := inspector.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	if healthErr := inspector.DependencyHealth(ctx); healthErr == nil {
		t.Fatal("DependencyHealth() error = nil, want broker initialization failure")
	}

	select {
	case observation := <-observed:
		if observation.Kind != ObservationBrokerConnect ||
			observation.ClientID != "wired-inspector" {
			t.Fatalf("wired broker observation = %#v", observation)
		}
	default:
		t.Fatal("broker connection was not observed")
	}
	if reentryErr := <-reentryResult; !errors.Is(
		reentryErr,
		ErrObserverReentry,
	) {
		t.Fatalf("observer Close() error = %v, want %v", reentryErr, ErrObserverReentry)
	}
}

func mustNormalizeObserverPolicy(
	t *testing.T,
	policy ObserverPolicy,
) ObserverPolicy {
	t.Helper()

	normalized, err := normalizeObserverPolicy(policy)
	if err != nil {
		t.Fatalf("normalizeObserverPolicy() error = %v", err)
	}

	return normalized
}

func TestInspectorObservationValidationRejectsContradictoryMetadata(
	t *testing.T,
) {

	success := Observation{
		StartedAt: time.Unix(1, 0),
		Duration:  time.Millisecond,
		ClientID:  "inspector",
		Succeeded: true,
	}
	failure := success
	failure.Succeeded = false
	failure.Category = ErrorPermanent
	valid := []Observation{
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationInspectorCluster
			value.BrokerCount = 1
		}),
		mutatedObservation(failure, func(value *Observation) {
			value.Kind = ObservationInspectorCluster
		}),
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationInspectorTopics
			value.TopicCount = 1
			value.PartitionCount = 1
		}),
		mutatedObservation(failure, func(value *Observation) {
			value.Kind = ObservationInspectorTopics
			value.TopicCount = 1
		}),
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationInspectorConsumerGroups
			value.GroupCount = 1
			value.GroupMemberCount = 1
			value.PartitionCount = 1
		}),
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationInspectorConsumerGroups
			value.GroupCount = 1
		}),
		mutatedObservation(failure, func(value *Observation) {
			value.Kind = ObservationInspectorConsumerGroups
			value.GroupCount = 1
		}),
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationDependencyHealth
			value.DependencyHealthy = true
		}),
		mutatedObservation(failure, func(value *Observation) {
			value.Kind = ObservationDependencyHealth
		}),
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationReadiness
			value.DependencyHealthy = true
			value.Ready = true
			value.ConsecutiveSuccesses = 1
		}),
		mutatedObservation(failure, func(value *Observation) {
			value.Kind = ObservationReadiness
			value.Ready = true
			value.ConsecutiveFailures = 1
		}),
		mutatedObservation(success, func(value *Observation) {
			value.Kind = ObservationInspectorShutdown
		}),
	}
	for index, observation := range valid {
		if err := observation.Validate(); err != nil {
			t.Fatalf("valid inspector observation %d error = %v", index, err)
		}
	}

	cluster := valid[0]
	topics := valid[2]
	groups := valid[4]
	health := valid[7]
	readiness := valid[9]
	shutdown := valid[11]
	record := Observation{
		Kind:        ObservationProduceRecord,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		RecordCount: 1,
		Succeeded:   true,
	}
	tests := []struct {
		name        string
		observation Observation
	}{
		{"negative broker count", mutatedObservation(cluster, func(value *Observation) {
			value.BrokerCount = -1
		})},
		{"negative topic count", mutatedObservation(topics, func(value *Observation) {
			value.TopicCount = -1
		})},
		{"negative group count", mutatedObservation(groups, func(value *Observation) {
			value.GroupCount = -1
		})},
		{"negative member count", mutatedObservation(groups, func(value *Observation) {
			value.GroupMemberCount = -1
		})},
		{"negative readiness failures", mutatedObservation(readiness, func(value *Observation) {
			value.ConsecutiveFailures = -1
		})},
		{"negative readiness successes", mutatedObservation(readiness, func(value *Observation) {
			value.ConsecutiveSuccesses = -1
		})},
		{"too many brokers", mutatedObservation(cluster, func(value *Observation) {
			value.BrokerCount = 10_001
		})},
		{"too many topics", mutatedObservation(topics, func(value *Observation) {
			value.TopicCount = 65
		})},
		{"too many groups", mutatedObservation(groups, func(value *Observation) {
			value.GroupCount = 65
		})},
		{"too many members", mutatedObservation(groups, func(value *Observation) {
			value.GroupMemberCount = 100_001
		})},
		{"too many partitions", mutatedObservation(groups, func(value *Observation) {
			value.PartitionCount = 1_000_001
		})},
		{"too many failures", mutatedObservation(readiness, func(value *Observation) {
			value.ConsecutiveFailures = 101
		})},
		{"too many successes", mutatedObservation(readiness, func(value *Observation) {
			value.ConsecutiveSuccesses = 101
		})},
		{"cluster missing brokers", mutatedObservation(cluster, func(value *Observation) {
			value.BrokerCount = 0
		})},
		{"failed cluster with brokers", mutatedObservation(valid[1], func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"cluster topic count", mutatedObservation(cluster, func(value *Observation) {
			value.TopicCount = 1
		})},
		{"cluster group count", mutatedObservation(cluster, func(value *Observation) {
			value.GroupCount = 1
		})},
		{"cluster member count", mutatedObservation(cluster, func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"cluster partition count", mutatedObservation(cluster, func(value *Observation) {
			value.PartitionCount = 1
		})},
		{"cluster dependency state", mutatedObservation(cluster, func(value *Observation) {
			value.DependencyHealthy = true
		})},
		{"cluster readiness state", mutatedObservation(cluster, func(value *Observation) {
			value.Ready = true
		})},
		{"cluster failure streak", mutatedObservation(cluster, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"cluster success streak", mutatedObservation(cluster, func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
		{"topics missing target", mutatedObservation(topics, func(value *Observation) {
			value.TopicCount = 0
		})},
		{"topics missing partitions", mutatedObservation(topics, func(value *Observation) {
			value.PartitionCount = 0
		})},
		{"failed topics with partitions", mutatedObservation(valid[3], func(value *Observation) {
			value.PartitionCount = 1
		})},
		{"topics broker count", mutatedObservation(topics, func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"topics group count", mutatedObservation(topics, func(value *Observation) {
			value.GroupCount = 1
		})},
		{"topics member count", mutatedObservation(topics, func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"topics dependency state", mutatedObservation(topics, func(value *Observation) {
			value.DependencyHealthy = true
		})},
		{"topics readiness state", mutatedObservation(topics, func(value *Observation) {
			value.Ready = true
		})},
		{"topics failure streak", mutatedObservation(topics, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"topics success streak", mutatedObservation(topics, func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
		{"groups missing target", mutatedObservation(groups, func(value *Observation) {
			value.GroupCount = 0
		})},
		{"failed groups with members", mutatedObservation(valid[6], func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"failed groups with partitions", mutatedObservation(valid[6], func(value *Observation) {
			value.PartitionCount = 1
		})},
		{"groups broker count", mutatedObservation(groups, func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"groups topic count", mutatedObservation(groups, func(value *Observation) {
			value.TopicCount = 1
		})},
		{"groups dependency state", mutatedObservation(groups, func(value *Observation) {
			value.DependencyHealthy = true
		})},
		{"groups readiness state", mutatedObservation(groups, func(value *Observation) {
			value.Ready = true
		})},
		{"groups failure streak", mutatedObservation(groups, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"groups success streak", mutatedObservation(groups, func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
		{"health broker count", mutatedObservation(health, func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"health topic count", mutatedObservation(health, func(value *Observation) {
			value.TopicCount = 1
		})},
		{"health group count", mutatedObservation(health, func(value *Observation) {
			value.GroupCount = 1
		})},
		{"health member count", mutatedObservation(health, func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"health partition count", mutatedObservation(health, func(value *Observation) {
			value.PartitionCount = 1
		})},
		{"health readiness state", mutatedObservation(health, func(value *Observation) {
			value.Ready = true
		})},
		{"health failure streak", mutatedObservation(health, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"health success streak", mutatedObservation(health, func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
		{"health contradictory outcome", mutatedObservation(health, func(value *Observation) {
			value.DependencyHealthy = false
		})},
		{"failed health reports healthy", mutatedObservation(valid[8], func(value *Observation) {
			value.DependencyHealthy = true
		})},
		{"readiness broker count", mutatedObservation(readiness, func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"readiness topic count", mutatedObservation(readiness, func(value *Observation) {
			value.TopicCount = 1
		})},
		{"readiness group count", mutatedObservation(readiness, func(value *Observation) {
			value.GroupCount = 1
		})},
		{"readiness member count", mutatedObservation(readiness, func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"readiness partition count", mutatedObservation(readiness, func(value *Observation) {
			value.PartitionCount = 1
		})},
		{"healthy readiness has failures", mutatedObservation(readiness, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"healthy readiness lacks successes", mutatedObservation(readiness, func(value *Observation) {
			value.ConsecutiveSuccesses = 0
		})},
		{"healthy readiness failed operation", mutatedObservation(readiness, func(value *Observation) {
			value.Succeeded = false
			value.Category = ErrorPermanent
		})},
		{"unhealthy readiness lacks failures", mutatedObservation(valid[10], func(value *Observation) {
			value.ConsecutiveFailures = 0
		})},
		{"unhealthy readiness has successes", mutatedObservation(valid[10], func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
		{"unhealthy readiness succeeded operation", mutatedObservation(valid[10], func(value *Observation) {
			value.Succeeded = true
			value.Category = ErrorUnknown
		})},
		{"shutdown failed", mutatedObservation(shutdown, func(value *Observation) {
			value.Succeeded = false
			value.Category = ErrorPermanent
		})},
		{"shutdown broker count", mutatedObservation(shutdown, func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"shutdown topic count", mutatedObservation(shutdown, func(value *Observation) {
			value.TopicCount = 1
		})},
		{"shutdown group count", mutatedObservation(shutdown, func(value *Observation) {
			value.GroupCount = 1
		})},
		{"shutdown member count", mutatedObservation(shutdown, func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"shutdown partition count", mutatedObservation(shutdown, func(value *Observation) {
			value.PartitionCount = 1
		})},
		{"shutdown dependency state", mutatedObservation(shutdown, func(value *Observation) {
			value.DependencyHealthy = true
		})},
		{"shutdown readiness state", mutatedObservation(shutdown, func(value *Observation) {
			value.Ready = true
		})},
		{"shutdown failure streak", mutatedObservation(shutdown, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"shutdown success streak", mutatedObservation(shutdown, func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
		{"record broker count", mutatedObservation(record, func(value *Observation) {
			value.BrokerCount = 1
		})},
		{"record topic count", mutatedObservation(record, func(value *Observation) {
			value.TopicCount = 1
		})},
		{"record group count", mutatedObservation(record, func(value *Observation) {
			value.GroupCount = 1
		})},
		{"record member count", mutatedObservation(record, func(value *Observation) {
			value.GroupMemberCount = 1
		})},
		{"record dependency state", mutatedObservation(record, func(value *Observation) {
			value.DependencyHealthy = true
		})},
		{"record readiness state", mutatedObservation(record, func(value *Observation) {
			value.Ready = true
		})},
		{"record failure streak", mutatedObservation(record, func(value *Observation) {
			value.ConsecutiveFailures = 1
		})},
		{"record success streak", mutatedObservation(record, func(value *Observation) {
			value.ConsecutiveSuccesses = 1
		})},
	}
	for _, test := range tests {
		if err := test.observation.Validate(); !errors.Is(
			err,
			ErrInvalidObservation,
		) {
			t.Fatalf("invalid inspector observation %q error = %v", test.name, err)
		}
	}
}

func mutatedObservation(
	observation Observation,
	mutate func(*Observation),
) Observation {
	mutate(&observation)

	return observation
}
