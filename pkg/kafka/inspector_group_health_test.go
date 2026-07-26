package kafka

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestInspectorReturnsBoundedConsumerGroupMembersAndAssignments(t *testing.T) {
	t.Parallel()

	instanceID := "worker-1"
	backend := &metadataInspectorBackend{
		recordingInspectorBackend: recordingInspectorBackend{
			groupLags: inspectorGroupLags{
				"orders-v1": {
					group:         "orders-v1",
					coordinatorID: 4,
					state:         "Stable",
					protocolType:  "consumer",
					protocol:      "cooperative-sticky",
					members: []inspectorGroupMember{
						{
							memberID:          "member-1",
							instanceID:        &instanceID,
							clientID:          "orders-worker",
							clientHost:        "/10.0.0.8",
							assignmentDecoded: true,
							assignments: map[string][]int32{
								"audit":  {1},
								"orders": {2, 0},
							},
						},
						{
							memberID:          "member-0",
							clientID:          "orders-worker",
							clientHost:        "/10.0.0.9",
							assignmentDecoded: true,
							assignments: map[string][]int32{
								"orders": {3},
							},
						},
					},
				},
			},
		},
	}
	inspector := inspectorWithMetadataBackend(backend)

	groups, err := inspector.ConsumerGroupLag(context.Background(), "orders-v1")
	if err != nil {
		t.Fatalf("ConsumerGroupLag() error = %v", err)
	}
	want := []ConsumerGroupState{{
		Group:         "orders-v1",
		CoordinatorID: 4,
		State:         "Stable",
		ProtocolType:  "consumer",
		Protocol:      "cooperative-sticky",
		Members: []ConsumerGroupMemberState{
			{
				MemberID:   "member-0",
				ClientID:   "orders-worker",
				ClientHost: "/10.0.0.9",
				Assignments: []TopicPartition{
					{Topic: "orders", Partition: 3},
				},
			},
			{
				MemberID:          "member-1",
				InstanceID:        "worker-1",
				InstanceIDVisible: true,
				ClientID:          "orders-worker",
				ClientHost:        "/10.0.0.8",
				Assignments: []TopicPartition{
					{Topic: "audit", Partition: 1},
					{Topic: "orders", Partition: 0},
					{Topic: "orders", Partition: 2},
				},
			},
		},
		Partitions: []ConsumerGroupPartitionLag{},
	}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("ConsumerGroupLag() = %#v, want %#v", groups, want)
	}

	backend.groupLags["orders-v1"].members[0].assignments["audit"][0] = 9
	if groups[0].Members[1].Assignments[0].Partition != 1 {
		t.Fatalf("ConsumerGroupLag() returned aliased assignments = %#v", groups)
	}
}

func TestInspectorSortsMultipleConsumerGroupResults(t *testing.T) {
	t.Parallel()

	backend := &metadataInspectorBackend{
		recordingInspectorBackend: recordingInspectorBackend{
			groupLags: inspectorGroupLags{
				"z-group": {
					group: "z-group", coordinatorID: 2, state: "Empty",
				},
				"a-group": {
					group: "a-group", coordinatorID: 1, state: "Empty",
				},
			},
		},
	}
	inspector := inspectorWithMetadataBackend(backend)

	groups, err := inspector.ConsumerGroupLag(
		context.Background(),
		"z-group",
		"a-group",
	)
	if err != nil {
		t.Fatalf("ConsumerGroupLag() error = %v", err)
	}
	if len(groups) != 2 ||
		groups[0].Group != "a-group" ||
		groups[1].Group != "z-group" {
		t.Fatalf("ConsumerGroupLag() = %#v", groups)
	}
}

func TestInspectorReadinessUsesHysteresisWithoutAffectingLiveness(t *testing.T) {
	t.Parallel()

	dependencyErr := errors.New("dependency unavailable")
	outcomes := []error{nil, nil, dependencyErr, dependencyErr, dependencyErr, nil, nil}
	backend := &metadataInspectorBackend{}
	backend.healthFn = func(context.Context) error {
		outcome := outcomes[0]
		outcomes = outcomes[1:]

		return outcome
	}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  3,
		RecoveryThreshold: 2,
	}

	assertReadiness := func(
		wantReady bool,
		wantDependency bool,
		wantFailures int,
		wantSuccesses int,
		wantErr error,
	) {
		t.Helper()

		state, err := inspector.Readiness(context.Background())
		if !errors.Is(err, wantErr) {
			t.Fatalf("Readiness() error = %v, want %v", err, wantErr)
		}
		want := ReadinessState{
			Ready:                wantReady,
			DependencyHealthy:    wantDependency,
			ConsecutiveFailures:  wantFailures,
			ConsecutiveSuccesses: wantSuccesses,
		}
		if state != want {
			t.Fatalf("Readiness() = %#v, want %#v", state, want)
		}
		if liveness := inspector.Liveness(); !liveness.Live {
			t.Fatalf("Liveness() changed after dependency result = %#v", liveness)
		}
	}

	assertReadiness(false, true, 0, 1, nil)
	assertReadiness(true, true, 0, 2, nil)
	assertReadiness(true, false, 1, 0, dependencyErr)
	assertReadiness(true, false, 2, 0, dependencyErr)
	assertReadiness(false, false, 3, 0, dependencyErr)
	assertReadiness(false, true, 0, 1, nil)
	assertReadiness(true, true, 0, 2, nil)
}

func TestInspectorReadinessIgnoresInvalidOrCanceledObservations(t *testing.T) {
	t.Parallel()

	backend := &metadataInspectorBackend{}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  2,
		RecoveryThreshold: 1,
	}

	ready, err := inspector.Readiness(context.Background())
	if err != nil || !ready.Ready {
		t.Fatalf("initial Readiness() = %#v, %v", ready, err)
	}

	nilState, err := inspector.Readiness(nil)
	if !errors.Is(err, ErrContextRequired) || nilState != ready {
		t.Fatalf("Readiness(nil) = %#v, %v, want %#v", nilState, err, ready)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledState, err := inspector.Readiness(canceled)
	if !errors.Is(err, context.Canceled) || canceledState != ready {
		t.Fatalf(
			"Readiness(canceled) = %#v, %v, want %#v",
			canceledState,
			err,
			ready,
		)
	}
}

func TestInspectorCloseImmediatelyFencesAllHealthSignals(t *testing.T) {
	t.Parallel()

	healthCalls := 0
	backend := &metadataInspectorBackend{}
	backend.healthFn = func(context.Context) error {
		healthCalls++

		return nil
	}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  1,
		RecoveryThreshold: 1,
	}
	if state, err := inspector.Readiness(context.Background()); err != nil ||
		!state.Ready {
		t.Fatalf("initial Readiness() = %#v, %v", state, err)
	}

	inspector.Close()
	inspector.Close()

	if state := inspector.Liveness(); state.Live {
		t.Fatalf("Liveness() after Close() = %#v", state)
	}
	if err := inspector.DependencyHealth(context.Background()); !errors.Is(
		err,
		ErrInspectorClosed,
	) {
		t.Fatalf("DependencyHealth() after Close() error = %v", err)
	}
	state, err := inspector.Readiness(context.Background())
	if !errors.Is(err, ErrInspectorClosed) ||
		state != (ReadinessState{}) {
		t.Fatalf("Readiness() after Close() = %#v, %v", state, err)
	}
	if backend.closed != 1 || healthCalls != 1 {
		t.Fatalf(
			"close/health calls = %d/%d, want 1/1",
			backend.closed,
			healthCalls,
		)
	}
}

func TestInspectorCloseFencesAnInFlightReadinessObservation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	backend := &metadataInspectorBackend{}
	backend.healthFn = func(context.Context) error {
		close(started)
		<-release

		return nil
	}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  1,
		RecoveryThreshold: 1,
	}
	result := make(chan struct {
		state ReadinessState
		err   error
	}, 1)
	go func() {
		state, err := inspector.Readiness(context.Background())
		result <- struct {
			state ReadinessState
			err   error
		}{state: state, err: err}
	}()

	<-started
	inspector.Close()
	close(release)
	observation := <-result
	if !errors.Is(observation.err, ErrInspectorClosed) ||
		observation.state != (ReadinessState{}) {
		t.Fatalf(
			"in-flight Readiness() = %#v, %v",
			observation.state,
			observation.err,
		)
	}
}

func TestInspectorCloseFencesReadinessWaitingToRecordObservation(t *testing.T) {
	t.Parallel()

	probed := make(chan struct{})
	backend := &metadataInspectorBackend{}
	backend.healthFn = func(context.Context) error {
		close(probed)

		return nil
	}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  1,
		RecoveryThreshold: 1,
	}
	inspector.readinessMu.Lock()
	result := make(chan struct {
		state ReadinessState
		err   error
	}, 1)
	go func() {
		state, err := inspector.Readiness(context.Background())
		result <- struct {
			state ReadinessState
			err   error
		}{state: state, err: err}
	}()
	<-probed

	closed := make(chan struct{})
	go func() {
		inspector.Close()
		close(closed)
	}()
	for inspector.Liveness().Live {
		runtime.Gosched()
	}
	inspector.readinessMu.Unlock()

	observation := <-result
	if !errors.Is(observation.err, ErrInspectorClosed) ||
		observation.state != (ReadinessState{}) {
		t.Fatalf(
			"blocked Readiness() = %#v, %v",
			observation.state,
			observation.err,
		)
	}
	<-closed
}

func TestReadinessPolicyDefaultsAndValidatesBounds(t *testing.T) {
	t.Parallel()

	config, err := normalizeInspectorConfig(InspectorConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "inspector",
	})
	if err != nil {
		t.Fatalf("normalizeInspectorConfig() error = %v", err)
	}
	if config.MaxGroupMembers != 10_000 ||
		config.Readiness != (ReadinessPolicy{
			FailureThreshold:  3,
			RecoveryThreshold: 2,
		}) {
		t.Fatalf("inspector group/readiness defaults = %#v", config)
	}
	if err := config.Readiness.Validate(); err != nil {
		t.Fatalf("ReadinessPolicy.Validate() error = %v", err)
	}
	if err := (ReadinessPolicy{}).Validate(); err != nil {
		t.Fatalf("zero ReadinessPolicy.Validate() error = %v", err)
	}

	for _, policy := range []ReadinessPolicy{
		{FailureThreshold: -1, RecoveryThreshold: 1},
		{FailureThreshold: 101, RecoveryThreshold: 1},
		{FailureThreshold: 1, RecoveryThreshold: -1},
		{FailureThreshold: 1, RecoveryThreshold: 101},
	} {
		if err := policy.Validate(); !errors.Is(
			err,
			ErrInvalidReadinessPolicy,
		) {
			t.Fatalf("ReadinessPolicy(%#v).Validate() error = %v", policy, err)
		}
	}

	invalidMembers := InspectorConfig{
		Brokers:         []string{"broker.internal:9092"},
		ClientID:        "inspector",
		MaxGroupMembers: 100_001,
	}
	if _, err := normalizeInspectorConfig(invalidMembers); !errors.Is(
		err,
		ErrInvalidInspectorConfig,
	) {
		t.Fatalf("normalize excessive MaxGroupMembers error = %v", err)
	}

	invalidReadiness := InspectorConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "inspector",
		Readiness: ReadinessPolicy{
			FailureThreshold:  101,
			RecoveryThreshold: 1,
		},
	}
	if _, err := normalizeInspectorConfig(invalidReadiness); !errors.Is(
		err,
		ErrInvalidReadinessPolicy,
	) {
		t.Fatalf("normalize invalid readiness error = %v", err)
	}
}

func TestConsumerAssignmentDecodeRejectsDuplicateTopics(t *testing.T) {
	t.Parallel()

	err := copyConsumerMemberAssignment(
		make(map[string][]int32),
		&kmsg.ConsumerMemberAssignment{Topics: []kmsg.ConsumerMemberAssignmentTopic{
			{Topic: "orders", Partitions: []int32{0}},
			{Topic: "orders", Partitions: []int32{1}},
		}},
	)
	if !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("copyConsumerMemberAssignment() error = %v", err)
	}
}

func TestConsumerAssignmentDecodeCopiesBrokerState(t *testing.T) {
	t.Parallel()

	assignment := &kmsg.ConsumerMemberAssignment{
		Topics: []kmsg.ConsumerMemberAssignmentTopic{{
			Topic: "orders", Partitions: []int32{2, 0},
		}},
	}
	member := newInspectorGroupMember(
		"member-1",
		nil,
		"client",
		"/host",
		assignment,
		true,
	)
	if member.assignmentErr != nil ||
		!member.assignmentDecoded ||
		!reflect.DeepEqual(member.assignments, map[string][]int32{
			"orders": {2, 0},
		}) {
		t.Fatalf("newInspectorGroupMember() = %#v", member)
	}

	assignment.Topics[0].Partitions[0] = 9
	if member.assignments["orders"][0] != 2 {
		t.Fatalf("assignment copy aliased broker state = %#v", member)
	}

	undecoded := newInspectorGroupMember(
		"member-2",
		nil,
		"client",
		"/host",
		nil,
		false,
	)
	if undecoded.assignmentDecoded || len(undecoded.assignments) != 0 {
		t.Fatalf("undecoded member = %#v", undecoded)
	}

	nilAssignment := newInspectorGroupMember(
		"member-3",
		nil,
		"client",
		"/host",
		nil,
		true,
	)
	if !errors.Is(nilAssignment.assignmentErr, ErrInvalidInspectionResponse) {
		t.Fatalf("nil assignment error = %v", nilAssignment.assignmentErr)
	}
}

type kadmGroupLagClientFunc func(
	context.Context,
	...string,
) (kadm.DescribedGroupLags, error)

func (function kadmGroupLagClientFunc) Lag(
	ctx context.Context,
	groups ...string,
) (kadm.DescribedGroupLags, error) {
	return function(ctx, groups...)
}

func TestFranzInspectorBackendTranslatesGroupLagStateAndErrors(t *testing.T) {
	t.Parallel()

	requestErr := errors.New("request failed")
	backend := &franzInspectorBackend{
		maxGroupMembers:       10,
		maxMetadataPartitions: 10,
		groupLags: kadmGroupLagClientFunc(func(
			context.Context,
			...string,
		) (kadm.DescribedGroupLags, error) {
			return kadm.DescribedGroupLags{
				"group": {
					Group: "group",
					State: "Stable",
					Members: []kadm.DescribedGroupMember{{
						MemberID: "member",
					}},
				},
			}, requestErr
		}),
	}

	lags, err := backend.Lag(context.Background(), "group")
	if !errors.Is(err, requestErr) ||
		lags["group"].group != "group" ||
		len(lags["group"].members) != 1 ||
		lags["group"].members[0].assignmentDecoded {
		t.Fatalf("Lag() = %#v, %v", lags, err)
	}
}

func TestConsumerAssignmentCopyBudgetBoundsTopicsAndPartitions(t *testing.T) {
	t.Parallel()

	used := 0
	if err := consumeGroupAssignmentCopyBudget(
		&used,
		3,
		nil,
		false,
	); err != nil || used != 0 {
		t.Fatalf("undecoded assignment budget = %d, %v", used, err)
	}
	if err := consumeGroupAssignmentCopyBudget(
		&used,
		3,
		nil,
		true,
	); !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("nil decoded assignment error = %v", err)
	}
	if err := consumeGroupAssignmentCopyBudget(
		&used,
		1,
		&kmsg.ConsumerMemberAssignment{
			Topics: []kmsg.ConsumerMemberAssignmentTopic{
				{Topic: "one"},
				{Topic: "two"},
			},
		},
		true,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("assignment topic budget error = %v", err)
	}
	if err := consumeGroupAssignmentCopyBudget(
		&used,
		2,
		&kmsg.ConsumerMemberAssignment{
			Topics: []kmsg.ConsumerMemberAssignmentTopic{{
				Topic: "one", Partitions: []int32{0, 1},
			}},
		},
		true,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("assignment partition budget error = %v", err)
	}
	used = 0
	if err := consumeGroupAssignmentCopyBudget(
		&used,
		3,
		&kmsg.ConsumerMemberAssignment{
			Topics: []kmsg.ConsumerMemberAssignmentTopic{{
				Topic: "one", Partitions: []int32{0, 1},
			}},
		},
		true,
	); err != nil || used != 3 {
		t.Fatalf("valid assignment budget = %d, %v", used, err)
	}
}

func TestGroupTranslationRejectsExcessiveDecodedAssignments(t *testing.T) {
	t.Parallel()

	lags, err := translateDescribedGroupLagsWithDecoder(
		kadm.DescribedGroupLags{
			"group": {
				Group: "group",
				Members: []kadm.DescribedGroupMember{{
					MemberID: "member",
				}},
			},
		},
		1,
		1,
		func(
			kadm.DescribedGroupMember,
		) (*kmsg.ConsumerMemberAssignment, bool) {
			return &kmsg.ConsumerMemberAssignment{
				Topics: []kmsg.ConsumerMemberAssignmentTopic{{
					Topic: "events", Partitions: []int32{0},
				}},
			}, true
		},
	)
	if lags != nil || !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("translateDescribedGroupLagsWithDecoder() = %#v, %v", lags, err)
	}
}

func TestFranzInspectorBackendBoundsGroupMetadataBeforeTranslation(t *testing.T) {
	t.Parallel()

	backend := &franzInspectorBackend{
		maxGroupMembers:       1,
		maxMetadataPartitions: 1,
		groupLags: kadmGroupLagClientFunc(func(
			context.Context,
			...string,
		) (kadm.DescribedGroupLags, error) {
			return kadm.DescribedGroupLags{
				"group": {
					Group: "group",
					Members: []kadm.DescribedGroupMember{
						{MemberID: "member-1"},
						{MemberID: "member-2"},
					},
				},
			}, nil
		}),
	}

	lags, err := backend.Lag(context.Background(), "group")
	if lags != nil || !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("Lag() = %#v, %v", lags, err)
	}
}

func TestInspectorRejectsInvalidOrExcessiveGroupMemberState(t *testing.T) {
	t.Parallel()

	instanceID := "instance-1"
	valid := func() inspectorGroupLags {
		return inspectorGroupLags{
			"group": {
				group:         "group",
				coordinatorID: 1,
				state:         "Stable",
				protocolType:  "consumer",
				protocol:      "cooperative-sticky",
				members: []inspectorGroupMember{{
					memberID:          "member-1",
					instanceID:        &instanceID,
					clientID:          "client-1",
					clientHost:        "/host",
					assignmentDecoded: true,
					assignments: map[string][]int32{
						"events": {0},
					},
				}},
			},
		}
	}

	for _, test := range []struct {
		name   string
		change func(*Inspector, inspectorGroupLags)
		want   error
	}{
		{
			name: "invalid coordinator",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.coordinatorID = -1
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid state",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.state = " Stable "
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid protocol",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.protocol = " sticky "
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "unsupported group protocol",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.protocolType = "connect"
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "unsupported empty group protocol",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.protocolType = "connect"
				group.protocol = ""
				group.members = nil
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing member assignor",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.protocol = ""
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "assignor without protocol type",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.protocolType = ""
				group.members = nil
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "members in empty group",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.state = "Empty"
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "members in dead group",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				group.state = "Dead"
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "member limit",
			change: func(inspector *Inspector, lags inspectorGroupLags) {
				inspector.maxGroupMembers = 1
				group := lags["group"]
				second := group.members[0]
				second.memberID = "member-2"
				second.instanceID = nil
				second.assignments = map[string][]int32{"events": {1}}
				group.members = append(group.members, second)
				lags["group"] = group
			},
			want: ErrInspectionResponseTooLarge,
		},
		{
			name: "invalid member ID",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].memberID = ""
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate member ID",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				second := group.members[0]
				second.instanceID = nil
				second.assignments = map[string][]int32{"events": {1}}
				group.members = append(group.members, second)
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate instance ID",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				group := lags["group"]
				second := group.members[0]
				second.memberID = "member-2"
				second.assignments = map[string][]int32{"events": {1}}
				group.members = append(group.members, second)
				lags["group"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid instance ID",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				invalid := " instance "
				lags["group"].members[0].instanceID = &invalid
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid client host",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].clientHost = "/host\nsecret"
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "undecoded assignment",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].assignmentDecoded = false
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "assignment decode error",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].assignmentErr =
					ErrInvalidInspectionResponse
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid assignment topic",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].assignments =
					map[string][]int32{"bad topic": {0}}
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative assignment partition",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].assignments["events"][0] = -1
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "empty assignment partition list",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].assignments["events"] = nil
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate assignment",
			change: func(_ *Inspector, lags inspectorGroupLags) {
				lags["group"].members[0].assignments["events"] =
					[]int32{0, 0}
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "assignment partition limit",
			change: func(inspector *Inspector, lags inspectorGroupLags) {
				inspector.maxMetadataPartitions = 1
				lags["group"].members[0].assignments["events"] =
					[]int32{0, 1}
			},
			want: ErrInspectionResponseTooLarge,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lags := valid()
			backend := &metadataInspectorBackend{
				recordingInspectorBackend: recordingInspectorBackend{
					groupLags: lags,
				},
			}
			inspector := inspectorWithMetadataBackend(backend)
			test.change(inspector, lags)
			if _, err := inspector.ConsumerGroupLag(
				context.Background(),
				"group",
			); !errors.Is(err, test.want) {
				t.Fatalf("ConsumerGroupLag() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestInspectorGroupLagErrorsAreDeterministic(t *testing.T) {
	t.Parallel()

	first := errors.New("first group failed")
	second := errors.New("second group failed")
	lags := inspectorGroupLags{
		"second": {group: "second", describeErr: second},
		"first":  {group: "first", describeErr: first},
	}
	for range 100 {
		if err := lags.err(); !errors.Is(err, first) {
			t.Fatalf("err() = %v, want %v", err, first)
		}
	}
}
