package kafka

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

func TestInspectorPreservesConsumerProtocolGroupPartialFailures(t *testing.T) {
	backend := &consumerProtocolInspectorTestBackend{
		metadataInspectorBackend: metadataInspectorBackend{},
		groups: inspectorConsumerProtocolGroups{
			"allowed": {
				group: "allowed", coordinatorID: 1, state: "Empty",
			},
		},
		groupErrors: map[string]error{"denied": kerr.GroupAuthorizationFailed},
	}
	inspector := &Inspector{
		admin:                    backend,
		client:                   backend,
		requestTimeout:           time.Second,
		maxMetadataPartitions:    100,
		maxGroupMembers:          10,
		maxConcurrentInspections: 2,
	}

	results, err := inspector.InspectConsumerProtocolGroups(
		context.Background(),
		"denied",
		"allowed",
	)
	if !errors.Is(err, ErrInspectionTargetsFailed) || len(results) != 2 {
		t.Fatalf("InspectConsumerProtocolGroups() = %#v, %v", results, err)
	}
	if results[0].Group != "denied" ||
		!errors.Is(results[0].Err, kerr.GroupAuthorizationFailed) ||
		results[0].Category != ErrorAuthorization ||
		results[1].Group != "allowed" ||
		results[1].Err != nil ||
		results[1].State.Group != "allowed" {
		t.Fatalf("InspectConsumerProtocolGroups() results = %#v", results)
	}
}

func TestInspectorReturnsBoundedConsumerProtocolGroupState(t *testing.T) {
	instanceID := "worker-1"
	rackID := "rack-a"
	regex := "orders-.*"
	backend := &consumerProtocolInspectorTestBackend{
		metadataInspectorBackend: metadataInspectorBackend{},
		groups: inspectorConsumerProtocolGroups{
			"orders-v2": {
				group:           "orders-v2",
				coordinatorID:   4,
				state:           "Stable",
				epoch:           7,
				assignmentEpoch: 7,
				assignor:        "uniform",
				members: []inspectorConsumerProtocolMember{
					{
						memberID:             "member-1",
						instanceID:           &instanceID,
						rackID:               &rackID,
						memberEpoch:          7,
						memberType:           1,
						clientID:             "orders-worker",
						clientHost:           "/10.0.0.8",
						subscribedTopicRegex: &regex,
						assignments: map[string][]int32{
							"orders-a": {2, 0},
						},
						targetAssignments: map[string][]int32{
							"orders-a": {0, 1, 2},
						},
					},
				},
				partitions: []ConsumerGroupPartitionLag{
					{
						Topic: "orders-a", Partition: 1,
						CommittedOffset: -1, StartOffset: 2, EndOffset: 8, Lag: 6,
					},
					{
						Topic: "orders-a", Partition: 0,
						CommittedOffset: 5, StartOffset: 2, EndOffset: 9, Lag: 4,
					},
				},
			},
		},
	}
	inspector := &Inspector{
		admin:                 backend,
		client:                backend,
		requestTimeout:        time.Second,
		maxMetadataPartitions: 100,
		maxGroupMembers:       10,
	}

	groups, err := inspector.ConsumerProtocolGroupLag(
		context.Background(),
		"orders-v2",
	)
	if err != nil {
		t.Fatalf("ConsumerProtocolGroupLag() error = %v", err)
	}
	want := []ConsumerProtocolGroupState{{
		Group:           "orders-v2",
		CoordinatorID:   4,
		State:           "Stable",
		Epoch:           7,
		AssignmentEpoch: 7,
		Assignor:        "uniform",
		Members: []ConsumerProtocolGroupMemberState{{
			MemberID:                    "member-1",
			InstanceID:                  "worker-1",
			InstanceIDVisible:           true,
			RackID:                      "rack-a",
			RackIDVisible:               true,
			MemberEpoch:                 7,
			MemberType:                  ConsumerProtocolMemberTypeConsumer,
			ClientID:                    "orders-worker",
			ClientHost:                  "/10.0.0.8",
			SubscribedTopics:            []string{},
			SubscribedTopicRegex:        "orders-.*",
			SubscribedTopicRegexVisible: true,
			Assignments: []TopicPartition{
				{Topic: "orders-a", Partition: 0},
				{Topic: "orders-a", Partition: 2},
			},
			TargetAssignments: []TopicPartition{
				{Topic: "orders-a", Partition: 0},
				{Topic: "orders-a", Partition: 1},
				{Topic: "orders-a", Partition: 2},
			},
		}},
		Partitions: []ConsumerGroupPartitionLag{
			{
				Topic: "orders-a", Partition: 0,
				CommittedOffset: 5, StartOffset: 2, EndOffset: 9, Lag: 4,
			},
			{
				Topic: "orders-a", Partition: 1,
				CommittedOffset: -1, StartOffset: 2, EndOffset: 8, Lag: 6,
			},
		},
	}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("ConsumerProtocolGroupLag() = %#v, want %#v", groups, want)
	}

	backend.groups["orders-v2"].members[0].assignments["orders-a"][0] = 9
	backend.groups["orders-v2"].partitions[0].EndOffset = 99
	if groups[0].Members[0].Assignments[1].Partition != 2 ||
		groups[0].Partitions[1].EndOffset != 8 {
		t.Fatalf("ConsumerProtocolGroupLag() returned aliased state = %#v", groups)
	}
}

func TestInspectorPreservesUnknownConsumerProtocolMemberType(t *testing.T) {
	backend := &consumerProtocolInspectorTestBackend{
		metadataInspectorBackend: metadataInspectorBackend{},
		groups: inspectorConsumerProtocolGroups{
			"group": {
				group: "group", coordinatorID: 1, state: "Stable",
				epoch: 1, assignmentEpoch: 1, assignor: "uniform",
				members: []inspectorConsumerProtocolMember{{
					memberID: "member", memberEpoch: 1, memberType: -1,
				}},
			},
		},
	}
	inspector := &Inspector{
		admin: backend, client: backend,
		requestTimeout:        time.Second,
		maxMetadataPartitions: 10,
		maxGroupMembers:       1,
	}

	groups, err := inspector.ConsumerProtocolGroupLag(
		context.Background(),
		"group",
	)
	if err != nil || len(groups) != 1 || len(groups[0].Members) != 1 ||
		groups[0].Members[0].MemberType != ConsumerProtocolMemberTypeUnknown {
		t.Fatalf("ConsumerProtocolGroupLag() = %#v, %v", groups, err)
	}
}

func TestFranzInspectorBackendBuildsConsumerProtocolLag(t *testing.T) {
	instanceID := "worker-1"
	rackID := "rack-a"
	client := &kadmConsumerProtocolTestClient{
		described: kadm.DescribedConsumerGroups{
			"orders-v2": {
				Group:       "orders-v2",
				Coordinator: kadm.BrokerDetail{NodeID: 3},
				State:       "Stable", Epoch: 4, AssignmentEpoch: 4,
				AssignorName: "uniform",
				Members: []kadm.ConsumerGroupMember{{
					MemberID: "member-1", InstanceID: &instanceID,
					RackID: &rackID, MemberEpoch: 4, MemberType: 1,
					ClientID: "orders", ClientHost: "/host",
					SubscribedTopics: []string{"orders"},
					Assignment: kadm.TopicsSet{
						"orders": map[int32]struct{}{0: {}},
					},
					TargetAssignment: kadm.TopicsSet{
						"orders": map[int32]struct{}{0: {}, 1: {}},
					},
				}},
			},
		},
		fetched: kadm.FetchOffsetsResponses{
			"orders-v2": {
				Group: "orders-v2",
				Fetched: kadm.OffsetResponses{
					"orders": map[int32]kadm.OffsetResponse{
						0: {Offset: kadm.Offset{Topic: "orders", Partition: 0, At: 5}},
						1: {Offset: kadm.Offset{Topic: "orders", Partition: 1, At: -1}},
					},
				},
			},
		},
		startOffsets: kadm.ListedOffsets{
			"orders": map[int32]kadm.ListedOffset{
				0: {Topic: "orders", Partition: 0, Offset: 2},
				1: {Topic: "orders", Partition: 1, Offset: 2},
			},
		},
		endOffsets: kadm.ListedOffsets{
			"orders": map[int32]kadm.ListedOffset{
				0: {Topic: "orders", Partition: 0, Offset: 9},
				1: {Topic: "orders", Partition: 1, Offset: 8},
			},
		},
	}
	backend := &franzInspectorBackend{
		consumerProtocolGroups: client,
		maxGroupMembers:        10,
		maxMetadataPartitions:  20,
	}

	groups, err := backend.ConsumerProtocolLag(context.Background(), "orders-v2")
	if err != nil {
		t.Fatalf("ConsumerProtocolLag() error = %v", err)
	}
	group := groups["orders-v2"]
	wantLag := []ConsumerGroupPartitionLag{
		{
			Topic: "orders", Partition: 0,
			CommittedOffset: 5, StartOffset: 2, EndOffset: 9, Lag: 4,
		},
		{
			Topic: "orders", Partition: 1,
			CommittedOffset: -1, StartOffset: 2, EndOffset: 8, Lag: 6,
		},
	}
	if group.group != "orders-v2" || group.coordinatorID != 3 ||
		len(group.members) != 1 ||
		!reflect.DeepEqual(group.partitions, wantLag) ||
		!reflect.DeepEqual(client.describedGroups, []string{"orders-v2"}) ||
		!reflect.DeepEqual(client.fetchedGroups, []string{"orders-v2"}) ||
		!reflect.DeepEqual(client.startTopics, []string{"orders"}) ||
		!reflect.DeepEqual(client.endTopics, []string{"orders"}) {
		t.Fatalf("ConsumerProtocolLag() = %#v; calls = %#v", groups, client)
	}

	client.described["orders-v2"].Members[0].SubscribedTopics[0] = "changed"
	client.described["orders-v2"].Members[0].Assignment["orders"][9] = struct{}{}
	if group.members[0].subscribedTopics[0] != "orders" ||
		len(group.members[0].assignments["orders"]) != 1 {
		t.Fatalf("ConsumerProtocolLag() aliased kadm state = %#v", group)
	}
}

func TestFranzInspectorBackendRejectsConsumerProtocolMetadataBeforeOffsetRequests(t *testing.T) {
	client := &kadmConsumerProtocolTestClient{
		described: kadm.DescribedConsumerGroups{
			"group": {
				Group:       "group",
				Coordinator: kadm.BrokerDetail{NodeID: 1},
				State:       "Stable", Epoch: 1, AssignmentEpoch: 1,
				AssignorName: "uniform",
				Members: []kadm.ConsumerGroupMember{{
					MemberID: "member", MemberEpoch: 1, MemberType: 1,
					ClientID: "client", ClientHost: "/host",
					Assignment: kadm.TopicsSet{
						"invalid topic": map[int32]struct{}{0: {}},
					},
				}},
			},
		},
	}
	backend := &franzInspectorBackend{
		consumerProtocolGroups: client,
		maxGroupMembers:        10,
		maxMetadataPartitions:  10,
	}

	groups, err := backend.ConsumerProtocolLag(context.Background(), "group")
	if groups != nil || !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("ConsumerProtocolLag() = %#v, %v", groups, err)
	}
	if client.fetchedGroups != nil || client.startTopics != nil || client.endTopics != nil {
		t.Fatalf("malformed metadata reached offset requests: %#v", client)
	}
}

func TestConsumerProtocolTranslationRejectsInvalidMetadataBeforeCopy(t *testing.T) {
	stringPointer := func(value string) *string { return &value }
	valid := func() kadm.DescribedConsumerGroup {
		return kadm.DescribedConsumerGroup{
			Group:       "group",
			Coordinator: kadm.BrokerDetail{NodeID: 1},
			State:       "Stable", Epoch: 1, AssignmentEpoch: 1,
			AssignorName: "uniform",
			Members: []kadm.ConsumerGroupMember{{
				MemberID: "member", MemberEpoch: 1, MemberType: 1,
				ClientID: "client", ClientHost: "/host",
				SubscribedTopics: []string{"topic"},
				Assignment: kadm.TopicsSet{
					"topic": map[int32]struct{}{0: {}},
				},
			}},
		}
	}
	tests := []struct {
		name   string
		mutate func(*kadm.DescribedConsumerGroup)
	}{
		{
			name: "group metadata",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.State = "STABLE"
			},
		},
		{
			name: "member metadata",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].MemberID = " member "
			},
		},
		{
			name: "instance id",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].InstanceID = stringPointer("")
			},
		},
		{
			name: "rack id",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].RackID = stringPointer("")
			},
		},
		{
			name: "subscription regex",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].SubscribedTopicRegex = stringPointer(" regex ")
			},
		},
		{
			name: "subscription topic",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].SubscribedTopics[0] = "invalid topic"
			},
		},
		{
			name: "empty assignment",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].Assignment["topic"] = map[int32]struct{}{}
			},
		},
		{
			name: "negative assignment partition",
			mutate: func(group *kadm.DescribedConsumerGroup) {
				group.Members[0].Assignment["topic"] = map[int32]struct{}{-1: {}}
			},
		},
	}
	if groups, err := translateConsumerProtocolGroups(
		kadm.DescribedConsumerGroups{"group": valid()},
		10,
		10,
	); err != nil || len(groups) != 1 {
		t.Fatalf("valid translation = %#v, %v", groups, err)
	}
	first := valid()
	second := valid()
	second.Group = "other"
	second.Members[0].MemberID = " invalid member "
	if groups, err := translateConsumerProtocolGroups(
		kadm.DescribedConsumerGroups{
			"group": first,
			"other": second,
		},
		1,
		10,
	); groups != nil || !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("aggregate member limit translation = %#v, %v", groups, err)
	}
	if count, err := consumerProtocolGroupMemberCount(
		kadm.DescribedConsumerGroups{
			"group": first,
			"other": second,
		},
		2,
	); err != nil || count != 2 {
		t.Fatalf("aggregate member count = %d, %v", count, err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			group := valid()
			test.mutate(&group)
			groups, err := translateConsumerProtocolGroups(
				kadm.DescribedConsumerGroups{"group": group},
				10,
				10,
			)
			if groups != nil || !errors.Is(err, ErrInvalidInspectionResponse) {
				t.Fatalf("translation = %#v, %v", groups, err)
			}
		})
	}
}

func TestFranzInspectorBackendRejectsDuplicateConsumerProtocolMetadataBeforeOffsetRequests(t *testing.T) {
	member := kadm.ConsumerGroupMember{
		MemberID: "member", MemberEpoch: 1, MemberType: 1,
		ClientID: "client", ClientHost: "/host",
	}
	client := &kadmConsumerProtocolTestClient{
		described: kadm.DescribedConsumerGroups{
			"group": {
				Group:       "group",
				Coordinator: kadm.BrokerDetail{NodeID: 1},
				State:       "Stable", Epoch: 1, AssignmentEpoch: 1,
				AssignorName: "uniform",
				Members:      []kadm.ConsumerGroupMember{member, member},
			},
		},
	}
	backend := &franzInspectorBackend{
		consumerProtocolGroups: client,
		maxGroupMembers:        10,
		maxMetadataPartitions:  10,
	}

	groups, err := backend.ConsumerProtocolLag(context.Background(), "group")
	if groups != nil || !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("ConsumerProtocolLag() = %#v, %v", groups, err)
	}
	if client.fetchedGroups != nil {
		t.Fatalf("duplicate metadata reached offset requests: %#v", client)
	}
}

func TestFranzInspectorBackendRejectsConsumerProtocolFailures(t *testing.T) {
	requestErr := errors.New("request failed")
	groupErr := errors.New("group failed")
	newClient := func() *kadmConsumerProtocolTestClient {
		return &kadmConsumerProtocolTestClient{
			described: kadm.DescribedConsumerGroups{
				"group": {
					Group: "group", Coordinator: kadm.BrokerDetail{NodeID: 1},
					State: "Empty",
				},
			},
			fetched: kadm.FetchOffsetsResponses{
				"group": {Group: "group", Fetched: kadm.OffsetResponses{}},
			},
		}
	}
	for _, test := range []struct {
		name    string
		prepare func(*kadmConsumerProtocolTestClient)
		client  bool
		want    error
	}{
		{
			name: "missing client", client: false,
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "describe request",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				client.describeErr = requestErr
			},
			client: true, want: requestErr,
		},
		{
			name: "missing described group",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				client.described = nil
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "mismatched described group",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				group := client.described["group"]
				group.Group = "other"
				client.described["group"] = group
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "translation member limit",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				group := client.described["group"]
				group.Members = []kadm.ConsumerGroupMember{
					{MemberID: "member-1"},
					{MemberID: "member-2"},
				}
				client.described["group"] = group
			},
			client: true, want: ErrInspectionResponseTooLarge,
		},
		{
			name: "described group error",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				group := client.described["group"]
				group.Err = groupErr
				client.described["group"] = group
			},
			client: true,
		},
		{
			name: "missing fetched group",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				client.fetched = nil
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "mismatched fetched group",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				client.fetched["group"] = kadm.FetchOffsetsResponse{Group: "other"}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected fetched group",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				client.fetched["extra"] = kadm.FetchOffsetsResponse{Group: "extra"}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "fetch error",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				client.fetched["group"] = kadm.FetchOffsetsResponse{
					Group: "group", Err: groupErr,
				}
			},
			client: true,
		},
		{
			name: "start offsets request",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				group := client.described["group"]
				group.State = "Stable"
				group.Epoch = 1
				group.AssignmentEpoch = 1
				group.AssignorName = "uniform"
				group.Members = []kadm.ConsumerGroupMember{{
					MemberID: "member", MemberEpoch: 1, MemberType: 1,
					ClientID: "client", ClientHost: "/host",
					SubscribedTopics: []string{"topic"},
				}}
				client.described["group"] = group
				client.startErr = requestErr
			},
			client: true, want: requestErr,
		},
		{
			name: "end offsets request",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				group := client.described["group"]
				group.State = "Stable"
				group.Epoch = 1
				group.AssignmentEpoch = 1
				group.AssignorName = "uniform"
				group.Members = []kadm.ConsumerGroupMember{{
					MemberID: "member", MemberEpoch: 1, MemberType: 1,
					ClientID: "client", ClientHost: "/host",
					SubscribedTopics: []string{"topic"},
				}}
				client.described["group"] = group
				client.endErr = requestErr
			},
			client: true, want: requestErr,
		},
		{
			name: "missing listed offsets",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				group := client.described["group"]
				group.State = "Stable"
				group.Epoch = 1
				group.AssignmentEpoch = 1
				group.AssignorName = "uniform"
				group.Members = []kadm.ConsumerGroupMember{{
					MemberID: "member", MemberEpoch: 1, MemberType: 1,
					ClientID: "client", ClientHost: "/host",
					Assignment: kadm.TopicsSet{
						"topic": map[int32]struct{}{0: {}},
					},
				}}
				client.described["group"] = group
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing start offsets rejected before metadata limit",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				group := client.described["group"]
				group.Members[0].SubscribedTopics = []string{"topic"}
				client.described["group"] = group
				client.startOffsets = nil
				for partition := int32(1); partition <= 10; partition++ {
					client.endOffsets["topic"][partition] = kadm.ListedOffset{
						Topic: "topic", Partition: partition, Offset: 1,
					}
				}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing end offsets",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				client.endOffsets = nil
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "mismatched committed offset coordinate",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				response := client.fetched["group"]
				response.Fetched["topic"][0] = kadm.OffsetResponse{
					Offset: kadm.Offset{Topic: "other", Partition: 0, At: 1},
				}
				client.fetched["group"] = response
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid committed offset topic",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				response := client.fetched["group"]
				response.Fetched = kadm.OffsetResponses{
					"invalid topic": map[int32]kadm.OffsetResponse{},
				}
				client.fetched["group"] = response
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "mismatched listed offset coordinate",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				client.startOffsets["topic"][0] = kadm.ListedOffset{
					Topic: "other", Partition: 0,
				}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected listed offset target",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				client.startOffsets = kadm.ListedOffsets{
					"other": map[int32]kadm.ListedOffset{},
				}
				client.endOffsets = kadm.ListedOffsets{
					"other": map[int32]kadm.ListedOffset{},
				}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "listed offset partition set mismatch",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				client.endOffsets["topic"][1] = kadm.ListedOffset{
					Topic: "topic", Partition: 1,
				}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected listed offset topic",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				client.startOffsets["extra"] = map[int32]kadm.ListedOffset{}
				client.endOffsets["extra"] = map[int32]kadm.ListedOffset{}
			},
			client: true, want: ErrInvalidInspectionResponse,
		},
		{
			name: "listed offset partition limit",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				for partition := int32(1); partition <= 10; partition++ {
					client.startOffsets["topic"][partition] = kadm.ListedOffset{
						Topic: "topic", Partition: partition,
					}
					client.endOffsets["topic"][partition] = kadm.ListedOffset{
						Topic: "topic", Partition: partition,
					}
				}
			},
			client: true, want: ErrInspectionResponseTooLarge,
		},
		{
			name: "listed offset broker error",
			prepare: func(client *kadmConsumerProtocolTestClient) {
				prepareValidConsumerProtocolOffsetFixture(client)
				client.startOffsets["topic"][0] = kadm.ListedOffset{
					Topic: "topic", Partition: 0, Err: requestErr,
				}
			},
			client: true, want: requestErr,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newClient()
			if test.prepare != nil {
				test.prepare(client)
			}
			backend := &franzInspectorBackend{
				maxGroupMembers: 1, maxMetadataPartitions: 10,
			}
			if test.client {
				backend.consumerProtocolGroups = client
			}
			groups, err := backend.ConsumerProtocolLag(
				context.Background(),
				"group",
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("ConsumerProtocolLag() = %#v, %v, want %v", groups, err, test.want)
			}
			if test.name == "described group error" &&
				!errors.Is(groups["group"].describeErr, groupErr) {
				t.Fatalf("describe error group = %#v", groups)
			}
			if test.name == "fetch error" &&
				!errors.Is(groups["group"].fetchErr, groupErr) {
				t.Fatalf("fetch error group = %#v", groups)
			}
		})
	}
}

func prepareValidConsumerProtocolOffsetFixture(client *kadmConsumerProtocolTestClient) {
	group := client.described["group"]
	group.State = "Stable"
	group.Epoch = 1
	group.AssignmentEpoch = 1
	group.AssignorName = "uniform"
	group.Members = []kadm.ConsumerGroupMember{{
		MemberID: "member", MemberEpoch: 1, MemberType: 1,
		ClientID: "client", ClientHost: "/host",
		Assignment: kadm.TopicsSet{
			"topic": map[int32]struct{}{0: {}},
		},
	}}
	client.described["group"] = group
	client.fetched["group"] = kadm.FetchOffsetsResponse{
		Group: "group",
		Fetched: kadm.OffsetResponses{
			"topic": map[int32]kadm.OffsetResponse{
				0: {Offset: kadm.Offset{Topic: "topic", Partition: 0, At: 1}},
			},
		},
	}
	client.startOffsets = kadm.ListedOffsets{
		"topic": map[int32]kadm.ListedOffset{
			0: {Topic: "topic", Partition: 0},
		},
	}
	client.endOffsets = kadm.ListedOffsets{
		"topic": map[int32]kadm.ListedOffset{
			0: {Topic: "topic", Partition: 0, Offset: 1},
		},
	}
}

func TestConsumerProtocolLagCalculationRetainsBrokerErrors(t *testing.T) {
	group := inspectorConsumerProtocolGroup{
		members: []inspectorConsumerProtocolMember{{
			assignments: map[string][]int32{"topic": {0}},
		}},
	}
	validStart := kadm.ListedOffsets{
		"topic": map[int32]kadm.ListedOffset{
			0: {Topic: "topic", Partition: 0, Offset: 1},
		},
	}
	validEnd := kadm.ListedOffsets{
		"topic": map[int32]kadm.ListedOffset{
			0: {Topic: "topic", Partition: 0, Offset: 2},
		},
	}
	validCommitted := kadm.OffsetResponses{
		"topic": map[int32]kadm.OffsetResponse{
			0: {Offset: kadm.Offset{Topic: "topic", Partition: 0, At: 1}},
		},
	}
	for _, test := range []struct {
		name      string
		committed kadm.OffsetResponses
		start     kadm.ListedOffsets
		end       kadm.ListedOffsets
		want      error
	}{
		{
			name: "commit error",
			committed: kadm.OffsetResponses{
				"topic": map[int32]kadm.OffsetResponse{0: {Err: requestErrForConsumerProtocol()}},
			},
			start: validStart, end: validEnd, want: requestErrForConsumerProtocol(),
		},
		{name: "missing start", committed: validCommitted, end: validEnd, want: ErrInvalidInspectionResponse},
		{name: "missing end", committed: validCommitted, start: validStart, want: ErrInvalidInspectionResponse},
		{
			name: "start error", committed: validCommitted,
			start: kadm.ListedOffsets{"topic": map[int32]kadm.ListedOffset{0: {Err: requestErrForConsumerProtocol()}}},
			end:   validEnd, want: requestErrForConsumerProtocol(),
		},
		{
			name: "end error", committed: validCommitted, start: validStart,
			end:  kadm.ListedOffsets{"topic": map[int32]kadm.ListedOffset{0: {Err: requestErrForConsumerProtocol()}}},
			want: requestErrForConsumerProtocol(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			lags, err := calculateConsumerProtocolGroupLag(
				group,
				test.committed,
				test.start,
				test.end,
				new(int),
				10,
			)
			if lags != nil || !errors.Is(err, test.want) {
				t.Fatalf("calculateConsumerProtocolGroupLag() = %#v, %v, want %v", lags, err, test.want)
			}
		})
	}
}

func TestFranzInspectorBackendSkipsFailedGroupDuringSharedLagCalculation(t *testing.T) {
	fetchErr := errors.New("fetch failed")
	client := &kadmConsumerProtocolTestClient{
		described: kadm.DescribedConsumerGroups{
			"failed": {
				Group: "failed", Coordinator: kadm.BrokerDetail{NodeID: 1},
				State: "Empty",
			},
			"healthy": {
				Group: "healthy", Coordinator: kadm.BrokerDetail{NodeID: 1},
				State: "Stable", Epoch: 1, AssignmentEpoch: 1,
				AssignorName: "uniform",
				Members: []kadm.ConsumerGroupMember{{
					MemberID: "member", MemberEpoch: 1, MemberType: 1,
					ClientID: "client", ClientHost: "/host",
					SubscribedTopics: []string{"topic"},
				}},
			},
		},
		fetched: kadm.FetchOffsetsResponses{
			"failed":  {Group: "failed", Err: fetchErr},
			"healthy": {Group: "healthy", Fetched: kadm.OffsetResponses{}},
		},
		startOffsets: kadm.ListedOffsets{
			"topic": map[int32]kadm.ListedOffset{
				0: {Topic: "topic", Partition: 0},
			},
		},
		endOffsets: kadm.ListedOffsets{
			"topic": map[int32]kadm.ListedOffset{
				0: {Topic: "topic", Partition: 0, Offset: 2},
			},
		},
	}
	backend := &franzInspectorBackend{
		consumerProtocolGroups: client,
		maxGroupMembers:        10,
		maxMetadataPartitions:  10,
	}

	groups, err := backend.ConsumerProtocolLag(
		context.Background(),
		"failed",
		"healthy",
	)
	if err != nil || !errors.Is(groups["failed"].fetchErr, fetchErr) ||
		len(groups["healthy"].partitions) != 1 {
		t.Fatalf("ConsumerProtocolLag() = %#v, %v", groups, err)
	}
	client.fetched["healthy"] = kadm.FetchOffsetsResponse{
		Group: "healthy",
		Fetched: kadm.OffsetResponses{
			"topic": map[int32]kadm.OffsetResponse{
				0: {Offset: kadm.Offset{Topic: "other", Partition: 0}},
			},
		},
	}
	groups, err = backend.ConsumerProtocolLag(
		context.Background(),
		"failed",
		"healthy",
	)
	if groups != nil || !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("invalid group after failed group = %#v, %v", groups, err)
	}
}

func requestErrForConsumerProtocol() error {
	return kerr.BrokerNotAvailable
}

func TestInspectorRejectsInvalidConsumerProtocolGroupState(t *testing.T) {
	stringPointer := func(value string) *string { return &value }
	valid := func() inspectorConsumerProtocolGroups {
		return inspectorConsumerProtocolGroups{
			"group": {
				group: "group", coordinatorID: 1, state: "Stable",
				epoch: 2, assignmentEpoch: 2, assignor: "uniform",
				members: []inspectorConsumerProtocolMember{{
					memberID: "member", instanceID: stringPointer("instance"),
					memberEpoch: 2, memberType: 1,
					clientID: "client", clientHost: "/host",
					subscribedTopics:  []string{"topic"},
					assignments:       map[string][]int32{"topic": {0}},
					targetAssignments: map[string][]int32{"topic": {0}},
				}},
				partitions: []ConsumerGroupPartitionLag{{
					Topic: "topic", Partition: 0, CommittedOffset: 1,
					StartOffset: 0, EndOffset: 2, Lag: 1,
				}},
			},
		}
	}
	for _, state := range []string{
		"Assigning", "Dead", "Empty", "Reconciling", "Stable",
	} {
		groups := valid()
		group := groups["group"]
		group.state = state
		groups["group"] = group
		inspector := &Inspector{maxMetadataPartitions: 100, maxGroupMembers: 10}
		if err := inspector.validateConsumerProtocolGroups(
			[]string{"group"},
			groups,
		); err != nil {
			t.Fatalf("valid state %q error = %v", state, err)
		}
	}
	boundaryGroups := valid()
	boundaryGroup := boundaryGroups["group"]
	boundaryGroup.coordinatorID = 0
	boundaryGroup.epoch = 0
	boundaryGroup.assignmentEpoch = 0
	boundaryGroup.members[0].memberEpoch = 0
	boundaryGroup.partitions[0] = ConsumerGroupPartitionLag{
		Topic: "topic", Partition: 0, CommittedOffset: 0,
		StartOffset: 1, EndOffset: 2, Lag: 2,
	}
	boundaryGroups["group"] = boundaryGroup
	boundaryInspector := &Inspector{maxMetadataPartitions: 6, maxGroupMembers: 1}
	if err := boundaryInspector.validateConsumerProtocolGroups(
		[]string{"group"},
		boundaryGroups,
	); err != nil {
		t.Fatalf("exact boundary group error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(inspectorConsumerProtocolGroups)
		limit  int
		want   error
	}{
		{
			name: "missing group",
			mutate: func(groups inspectorConsumerProtocolGroups) {
				delete(groups, "group")
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected map key",
			mutate: func(groups inspectorConsumerProtocolGroups) {
				groups["other"] = groups["group"]
				delete(groups, "group")
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "mismatched group",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.group = "other"
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative coordinator",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.coordinatorID = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "unknown state",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.state = "Unknown"
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "non-wire uppercase state",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.state = "STABLE"
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative group epoch",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.epoch = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative assignment epoch",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.assignmentEpoch = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "assignment ahead of group",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.assignmentEpoch = 3
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid assignor",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.assignor = " uniform "
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing active assignor",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.assignor = ""
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "member limit",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.members = append(group.members, group.members[0])
				group.members[1].memberID = "member-2"
				group.members[1].instanceID = stringPointer("instance-2")
				group.members[1].assignments = map[string][]int32{"topic": {1}}
				group.members[1].targetAssignments = map[string][]int32{"topic": {1}}
			}),
			limit: 100, want: ErrInspectionResponseTooLarge,
		},
		{
			name: "invalid member id",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.memberID = " member "
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative member epoch",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.memberEpoch = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "member epoch ahead of group",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.memberEpoch = 3
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid member type low",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.memberType = -2
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid member type high",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.memberType = 2
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid client id",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.clientID = " client "
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid client host",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.clientHost = "\n"
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate member id",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.members = append(group.members, group.members[0])
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid instance id",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.instanceID = stringPointer("")
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate instance id",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				second := group.members[0]
				second.memberID = "member-2"
				second.assignments = map[string][]int32{"topic": {1}}
				second.targetAssignments = map[string][]int32{"topic": {1}}
				group.members = append(group.members, second)
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid rack id",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.rackID = stringPointer(" rack ")
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid regex",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.subscribedTopicRegex = stringPointer(" regex ")
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid subscribed topic",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.subscribedTopics = []string{"bad topic"}
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate subscribed topic",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.subscribedTopics = []string{"topic", "topic"}
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid current assignment",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.assignments = map[string][]int32{"bad topic": {0}}
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid target assignment",
			mutate: mutateConsumerProtocolMember(func(member *inspectorConsumerProtocolMember) {
				member.targetAssignments = map[string][]int32{"topic": {-1}}
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate current owner",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				second := group.members[0]
				second.memberID = "member-2"
				second.instanceID = stringPointer("instance-2")
				group.members = append(group.members, second)
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name:   "metadata limit",
			mutate: func(inspectorConsumerProtocolGroups) {},
			limit:  5, want: ErrInspectionResponseTooLarge,
		},
		{
			name: "negative lag end offset",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].EndOffset = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid lag topic",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].Topic = "bad topic"
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative lag partition",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].Partition = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid committed offset",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].CommittedOffset = -2
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative start offset",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].StartOffset = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "end before start offset",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].StartOffset = 2
				group.partitions[0].EndOffset = 1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "negative reported lag",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].Lag = -1
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate lag",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions = append(group.partitions, group.partitions[0])
			}),
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "incorrect lag",
			mutate: mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
				group.partitions[0].Lag = 0
			}),
			want: ErrInvalidInspectionResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups := valid()
			test.mutate(groups)
			memberLimit := 10
			metadataLimit := 100
			if test.limit != 0 {
				if test.name == "member limit" {
					memberLimit = 1
				} else {
					metadataLimit = test.limit
				}
			}
			inspector := &Inspector{
				maxMetadataPartitions: metadataLimit,
				maxGroupMembers:       memberLimit,
			}
			err := inspector.validateConsumerProtocolGroups(
				[]string{"group"},
				groups,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("validateConsumerProtocolGroups() error = %v, want %v; groups = %#v", err, test.want, groups)
			}
		})
	}
}

func TestConsumerProtocolInspectionHandlesLifecycleErrorsAndObservations(t *testing.T) {
	describeErr := errors.New("describe failed")
	fetchErr := errors.New("fetch failed")
	if err := (inspectorConsumerProtocolGroup{describeErr: describeErr}).err(); !errors.Is(err, describeErr) {
		t.Fatalf("describe group error = %v", err)
	}
	if err := (inspectorConsumerProtocolGroup{fetchErr: fetchErr}).err(); !errors.Is(err, fetchErr) {
		t.Fatalf("fetch group error = %v", err)
	}
	ordered := inspectorConsumerProtocolGroups{
		"z": {group: "z"},
		"a": {group: "a"},
	}.sorted()
	if len(ordered) != 2 || ordered[0].group != "a" || ordered[1].group != "z" {
		t.Fatalf("sorted consumer protocol groups = %#v", ordered)
	}
	if err := (inspectorConsumerProtocolGroups{
		"group": {group: "group", fetchErr: fetchErr},
	}).err(); !errors.Is(err, fetchErr) {
		t.Fatalf("consumer protocol groups error = %v", err)
	}

	validState := inspectorConsumerProtocolGroup{
		group: "group", coordinatorID: 1, state: "Stable",
		epoch: 1, assignmentEpoch: 1, assignor: "uniform",
		members: []inspectorConsumerProtocolMember{
			{
				memberID: "member-b", memberEpoch: 1, memberType: 1,
				assignments:       map[string][]int32{"topic": {1}},
				targetAssignments: map[string][]int32{"topic": {1}},
			},
			{
				memberID: "member-a", memberEpoch: 1, memberType: 1,
				assignments:       map[string][]int32{"topic": {0}},
				targetAssignments: map[string][]int32{"topic": {0}},
			},
		},
		partitions: []ConsumerGroupPartitionLag{
			{Topic: "topic", Partition: 1, CommittedOffset: -1},
			{Topic: "topic", Partition: 0, CommittedOffset: -1},
		},
	}
	backend := &consumerProtocolInspectorTestBackend{
		metadataInspectorBackend: metadataInspectorBackend{},
		groups:                   inspectorConsumerProtocolGroups{"group": validState},
	}
	var observations []Observation
	inspector := &Inspector{
		admin:                    backend,
		client:                   backend,
		requestTimeout:           time.Second,
		maxMetadataPartitions:    100,
		maxGroupMembers:          10,
		maxConcurrentInspections: 2,
		observers: newObserverDispatcher(mustNormalizeObserverPolicy(t, ObserverPolicy{
			Observers: []ObserverFunc{func(
				_ context.Context,
				observation Observation,
			) error {
				observations = append(observations, observation)

				return nil
			}},
			FailureHandler: func(context.Context, ObservationFailure) {},
		})),
	}
	states, err := inspector.ConsumerProtocolGroupLag(context.Background(), "group")
	if err != nil || len(states) != 1 ||
		states[0].Members[0].MemberID != "member-a" ||
		states[0].Partitions[0].Partition != 0 {
		t.Fatalf("ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}
	if len(observations) != 1 || observations[0].GroupCount != 1 ||
		observations[0].GroupMemberCount != 2 ||
		observations[0].PartitionCount != 2 {
		t.Fatalf("consumer protocol observation = %#v", observations)
	}
	otherState := validState
	otherState.group = "other"
	backend.groups["other"] = otherState
	if states, err := inspector.ConsumerProtocolGroupLag(
		context.Background(),
		"group",
		"other",
	); err != nil || len(states) != 2 {
		t.Fatalf("multi-group ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}
	if len(observations) != 2 || observations[1].GroupCount != 2 ||
		observations[1].GroupMemberCount != 4 ||
		observations[1].PartitionCount != 4 {
		t.Fatalf("multi-group consumer protocol observation = %#v", observations)
	}
	delete(backend.groups, "other")
	results, err := inspector.InspectConsumerProtocolGroups(
		context.Background(),
		"group",
	)
	if err != nil || len(results) != 1 || results[0].State.Group != "group" {
		t.Fatalf("InspectConsumerProtocolGroups() = %#v, %v", results, err)
	}

	if states, err := inspector.ConsumerProtocolGroupLag(context.Background()); states != nil || !errors.Is(err, ErrInspectionTargetsRequired) {
		t.Fatalf("ConsumerProtocolGroupLag(no groups) = %#v, %v", states, err)
	}
	var nilContext context.Context
	if states, err := inspector.ConsumerProtocolGroupLag(nilContext, "group"); states != nil || !errors.Is(err, ErrContextRequired) {
		t.Fatalf("ConsumerProtocolGroupLag(nil) = %#v, %v", states, err)
	}
	if results, err := inspector.InspectConsumerProtocolGroups(context.Background()); results != nil || !errors.Is(err, ErrInspectionTargetsRequired) {
		t.Fatalf("InspectConsumerProtocolGroups(no groups) = %#v, %v", results, err)
	}
	if results, err := inspector.InspectConsumerProtocolGroups(nilContext, "group"); results != nil || !errors.Is(err, ErrContextRequired) {
		t.Fatalf("InspectConsumerProtocolGroups(nil) = %#v, %v", results, err)
	}

	unsupported := inspectorWithMetadataBackend(&metadataInspectorBackend{})
	if states, err := unsupported.ConsumerProtocolGroupLag(
		context.Background(),
		"group",
	); states != nil || !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("unsupported ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}

	backend.err = describeErr
	if states, err := inspector.ConsumerProtocolGroupLag(
		context.Background(),
		"group",
	); states != nil || !errors.Is(err, describeErr) {
		t.Fatalf("failed ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}
	backend.err = nil
	failedState := validState
	failedState.describeErr = describeErr
	backend.groups["group"] = failedState
	if states, err := inspector.ConsumerProtocolGroupLag(
		context.Background(),
		"group",
	); states != nil || !errors.Is(err, describeErr) {
		t.Fatalf("group-error ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}
	invalidState := validState
	invalidState.state = "invalid"
	backend.groups["group"] = invalidState
	if states, err := inspector.ConsumerProtocolGroupLag(
		context.Background(),
		"group",
	); states != nil || !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("invalid ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}

	backend.fn = func(ctx context.Context, _ ...string) (
		inspectorConsumerProtocolGroups,
		error,
	) {
		<-ctx.Done()

		return inspectorConsumerProtocolGroups{"group": validState}, nil
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if states, err := inspector.ConsumerProtocolGroupLag(cancelCtx, "group"); states != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ConsumerProtocolGroupLag() = %#v, %v", states, err)
	}
}

func TestConsumerProtocolTranslationAndAssignmentBudgets(t *testing.T) {
	described := kadm.DescribedConsumerGroups{
		"group": {
			Group: "group",
			Members: []kadm.ConsumerGroupMember{{
				MemberID:         "member",
				SubscribedTopics: []string{"topic"},
				Assignment: kadm.TopicsSet{
					"topic": map[int32]struct{}{0: {}},
				},
			}},
		},
	}
	if groups, err := translateConsumerProtocolGroups(described, 1, 1); groups != nil || !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("translateConsumerProtocolGroups() = %#v, %v", groups, err)
	}
	used := 2
	if err := consumeConsumerProtocolMemberBudget(
		&used,
		1,
		kadm.ConsumerGroupMember{},
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("consumeConsumerProtocolMemberBudget() error = %v", err)
	}

	owners := make(map[TopicPartition]struct{})
	used = 1
	if err := validateConsumerProtocolAssignments(
		map[string][]int32{"topic": {0}},
		owners,
		&used,
		1,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("assignment exhausted budget error = %v", err)
	}
	used = 0
	if err := validateConsumerProtocolAssignments(
		map[string][]int32{"topic": {0, 1}},
		owners,
		&used,
		2,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("assignment partition budget error = %v", err)
	}

	group := inspectorConsumerProtocolGroup{
		members: []inspectorConsumerProtocolMember{{
			assignments: map[string][]int32{"topic": {0}},
		}},
	}
	metadataEntries := 1
	if lags, err := calculateConsumerProtocolGroupLag(
		group,
		nil,
		kadm.ListedOffsets{
			"topic": map[int32]kadm.ListedOffset{0: {Topic: "topic", Partition: 0}},
		},
		kadm.ListedOffsets{
			"topic": map[int32]kadm.ListedOffset{0: {Topic: "topic", Partition: 0}},
		},
		&metadataEntries,
		1,
	); lags != nil || !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("lag partition budget = %#v, %v", lags, err)
	}
	for _, test := range []struct {
		name      string
		group     inspectorConsumerProtocolGroup
		committed kadm.OffsetResponses
		end       kadm.ListedOffsets
	}{
		{
			name: "target assignment",
			group: inspectorConsumerProtocolGroup{members: []inspectorConsumerProtocolMember{{
				targetAssignments: map[string][]int32{"topic": {0}},
			}}},
		},
		{
			name: "subscription",
			group: inspectorConsumerProtocolGroup{members: []inspectorConsumerProtocolMember{{
				subscribedTopics: []string{"topic"},
			}}},
			end: kadm.ListedOffsets{
				"topic": map[int32]kadm.ListedOffset{0: {Topic: "topic", Partition: 0}},
			},
		},
		{
			name: "committed offset",
			committed: kadm.OffsetResponses{
				"topic": map[int32]kadm.OffsetResponse{0: {}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			used := 0
			lags, err := calculateConsumerProtocolGroupLag(
				test.group,
				test.committed,
				nil,
				test.end,
				&used,
				0,
			)
			if lags != nil || !errors.Is(err, ErrInspectionResponseTooLarge) {
				t.Fatalf("lag %s budget = %#v, %v", test.name, lags, err)
			}
		})
	}
}

func TestConsumerProtocolHelperBoundaries(t *testing.T) {
	groups := inspectorConsumerProtocolGroups{
		"group": {
			members: []inspectorConsumerProtocolMember{{
				subscribedTopics: []string{"a", "b"},
				assignments: map[string][]int32{
					"a": {0, 1},
					"b": {0},
				},
				targetAssignments: map[string][]int32{"a": {0, 1}},
			}},
		},
	}
	if entries := consumerProtocolMetadataEntries(groups); entries != 10 {
		t.Fatalf("consumerProtocolMetadataEntries() = %d, want 10", entries)
	}
	if entries := consumerProtocolMetadataEntries(inspectorConsumerProtocolGroups{
		"group": {members: []inspectorConsumerProtocolMember{
			{subscribedTopics: []string{"a"}},
			{subscribedTopics: []string{"b"}},
		}},
	}); entries != 2 {
		t.Fatalf("subscribed topic metadata entries = %d, want 2", entries)
	}
	if entries := consumerProtocolMetadataEntries(inspectorConsumerProtocolGroups{
		"group": {members: []inspectorConsumerProtocolMember{{
			assignments: map[string][]int32{"a": {0, 1}},
		}}},
	}); entries != 3 {
		t.Fatalf("assignment metadata entries = %d, want 3", entries)
	}

	if err := validateConsumerProtocolDescriptionTargets(
		[]string{"group"},
		kadm.DescribedConsumerGroups{"group": {Group: "other"}},
	); !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("mismatched description target error = %v", err)
	}

	if err := validateConsumerProtocolListedOffsets(
		[]string{""},
		kadm.ListedOffsets{"": {0: {Topic: "", Partition: 0}}},
		kadm.ListedOffsets{"": {1: {Topic: "", Partition: 1}}},
		1,
	); !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("missing listed end partition error = %v", err)
	}
	listedStarts := kadm.ListedOffsets{
		"a": {0: {Topic: "a", Partition: 0}},
		"b": {0: {Topic: "b", Partition: 0}},
	}
	listedEnds := kadm.ListedOffsets{
		"a": {0: {Topic: "a", Partition: 0}},
		"b": {0: {Topic: "b", Partition: 0}},
	}
	if err := validateConsumerProtocolListedOffsets(
		[]string{"a", "b"},
		listedStarts,
		listedEnds,
		2,
	); err != nil {
		t.Fatalf("exact listed partition budget error = %v", err)
	}
	if err := validateConsumerProtocolListedOffsets(
		[]string{"a", "b"},
		listedStarts,
		listedEnds,
		1,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("cumulative listed partition budget error = %v", err)
	}

	used := 1
	member := kadm.ConsumerGroupMember{
		SubscribedTopics: []string{"topic"},
		Assignment: kadm.TopicsSet{
			"topic": map[int32]struct{}{0: {}},
		},
	}
	if err := consumeConsumerProtocolMemberBudget(&used, 4, member); err != nil || used != 4 {
		t.Fatalf("exact member metadata budget = %d, %v", used, err)
	}
	used = 4
	if err := consumeConsumerProtocolMemberBudget(
		&used,
		4,
		kadm.ConsumerGroupMember{},
	); err != nil || used != 4 {
		t.Fatalf("empty member at exact budget = %d, %v", used, err)
	}
	used = 3
	if err := consumeConsumerProtocolMemberBudget(
		&used,
		4,
		member,
	); !errors.Is(err, ErrInspectionResponseTooLarge) || used != 3 {
		t.Fatalf("overflowing member metadata budget = %d, %v", used, err)
	}

	errored := errors.New("group failed")
	described := kadm.DescribedConsumerGroups{
		"a-failed": {Group: "a-failed", Err: errored, Members: make([]kadm.ConsumerGroupMember, 3)},
		"b-active": {Group: "b-active", Members: []kadm.ConsumerGroupMember{{MemberID: "member"}}},
	}
	if count, err := consumerProtocolGroupMemberCount(described, 1); err != nil || count != 1 {
		t.Fatalf("member count excluding failed group = %d, %v", count, err)
	}
	described["c-active"] = kadm.DescribedConsumerGroup{
		Group: "c-active", Members: []kadm.ConsumerGroupMember{{MemberID: "member-2"}},
	}
	if count, err := consumerProtocolGroupMemberCount(described, 2); err != nil || count != 2 {
		t.Fatalf("exact aggregate member count = %d, %v", count, err)
	}
	if count, err := consumerProtocolGroupMemberCount(described, 1); count != 0 ||
		!errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("aggregate member overflow = %d, %v", count, err)
	}
	translated, err := translateConsumerProtocolGroups(
		kadm.DescribedConsumerGroups{
			"a-failed": {Group: "a-failed", Err: errored},
			"b-active": {
				Group: "b-active", Coordinator: kadm.BrokerDetail{NodeID: 0},
				State: "Empty",
			},
		},
		1,
		1,
	)
	if err != nil || len(translated) != 2 ||
		!errors.Is(translated["a-failed"].describeErr, errored) ||
		translated["b-active"].group != "b-active" {
		t.Fatalf("translation after failed group = %#v, %v", translated, err)
	}
	if err := validateConsumerProtocolGroupsWithLimits(
		[]string{"a-failed", "b-invalid"},
		inspectorConsumerProtocolGroups{
			"a-failed":  {group: "a-failed", describeErr: errored},
			"b-invalid": {group: "b-invalid", coordinatorID: -1, state: "Empty"},
		},
		1,
		1,
	); !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("invalid group after failed group error = %v", err)
	}
	activeGroup := inspectorConsumerProtocolGroup{
		coordinatorID: 0,
		state:         "Stable",
		assignor:      "uniform",
		members: []inspectorConsumerProtocolMember{{
			memberID: "member", memberType: int8(ConsumerProtocolMemberTypeUnknown),
		}},
	}
	firstActive := activeGroup
	firstActive.group = "a-active"
	secondActive := activeGroup
	secondActive.group = "b-active"
	if err := validateConsumerProtocolGroupsWithLimits(
		[]string{"a-active", "b-active"},
		inspectorConsumerProtocolGroups{
			"a-active": firstActive,
			"b-active": secondActive,
		},
		1,
		1,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("aggregate validation member limit error = %v", err)
	}

	topics := consumerProtocolLagTopics(
		inspectorConsumerProtocolGroups{
			"describe-failed": {
				group: "describe-failed", describeErr: errored,
				members: []inspectorConsumerProtocolMember{{subscribedTopics: []string{"hidden-a"}}},
			},
			"fetch-failed": {
				group: "fetch-failed", fetchErr: errored,
				members: []inspectorConsumerProtocolMember{{subscribedTopics: []string{"hidden-b"}}},
			},
			"healthy": {
				group: "healthy",
				members: []inspectorConsumerProtocolMember{{
					subscribedTopics:  []string{"subscribed"},
					assignments:       map[string][]int32{"assigned": {0}},
					targetAssignments: map[string][]int32{"target": {0}},
				}},
			},
		},
		kadm.FetchOffsetsResponses{
			"healthy": {Group: "healthy", Fetched: kadm.OffsetResponses{"committed": {}}},
		},
	)
	if want := []string{"assigned", "committed", "subscribed", "target"}; !reflect.DeepEqual(topics, want) {
		t.Fatalf("consumerProtocolLagTopics() = %#v, want %#v", topics, want)
	}

	metadataEntries := 0
	lags, err := calculateConsumerProtocolGroupLag(
		inspectorConsumerProtocolGroup{members: []inspectorConsumerProtocolMember{{
			assignments: map[string][]int32{"topic": {0}},
		}}},
		nil,
		kadm.ListedOffsets{"topic": {0: {Topic: "topic", Partition: 0, Offset: 1}}},
		kadm.ListedOffsets{"topic": {0: {Topic: "topic", Partition: 0, Offset: 5}}},
		&metadataEntries,
		1,
	)
	if err != nil || metadataEntries != 1 || len(lags) != 1 ||
		lags[0].CommittedOffset != -1 || lags[0].Lag != 4 {
		t.Fatalf("uncommitted exact lag budget = %#v, entries %d, %v", lags, metadataEntries, err)
	}
	metadataEntries = 0
	lags, err = calculateConsumerProtocolGroupLag(
		inspectorConsumerProtocolGroup{members: []inspectorConsumerProtocolMember{{
			assignments: map[string][]int32{"topic": {0}},
		}}},
		kadm.OffsetResponses{"topic": {0: {Offset: kadm.Offset{Topic: "topic", Partition: 0, At: 0}}}},
		kadm.ListedOffsets{"topic": {0: {Topic: "topic", Partition: 0, Offset: 1}}},
		kadm.ListedOffsets{"topic": {0: {Topic: "topic", Partition: 0, Offset: 5}}},
		&metadataEntries,
		1,
	)
	if err != nil || len(lags) != 1 || lags[0].CommittedOffset != 0 || lags[0].Lag != 5 {
		t.Fatalf("zero committed offset lag = %#v, %v", lags, err)
	}

	owners := make(map[TopicPartition]struct{})
	metadataEntries = 0
	if err := validateConsumerProtocolAssignments(
		map[string][]int32{"topic": {0}},
		owners,
		&metadataEntries,
		2,
	); err != nil || metadataEntries != 2 || len(owners) != 1 {
		t.Fatalf("exact assignment budget = %d, %#v, %v", metadataEntries, owners, err)
	}
	metadataEntries = 2
	if err := validateConsumerProtocolAssignments(
		map[string][]int32{"other": {0}},
		owners,
		&metadataEntries,
		2,
	); !errors.Is(err, ErrInspectionResponseTooLarge) || metadataEntries != 2 {
		t.Fatalf("exhausted assignment budget = %d, %v", metadataEntries, err)
	}
}

func mutateConsumerProtocolGroup(
	mutate func(*inspectorConsumerProtocolGroup),
) func(inspectorConsumerProtocolGroups) {
	return func(groups inspectorConsumerProtocolGroups) {
		group := groups["group"]
		mutate(&group)
		groups["group"] = group
	}
}

func mutateConsumerProtocolMember(
	mutate func(*inspectorConsumerProtocolMember),
) func(inspectorConsumerProtocolGroups) {
	return mutateConsumerProtocolGroup(func(group *inspectorConsumerProtocolGroup) {
		mutate(&group.members[0])
	})
}

type kadmConsumerProtocolTestClient struct {
	described       kadm.DescribedConsumerGroups
	describeErr     error
	fetched         kadm.FetchOffsetsResponses
	startOffsets    kadm.ListedOffsets
	startErr        error
	endOffsets      kadm.ListedOffsets
	endErr          error
	describedGroups []string
	fetchedGroups   []string
	startTopics     []string
	endTopics       []string
}

func (client *kadmConsumerProtocolTestClient) DescribeConsumerGroups(
	_ context.Context,
	groups ...string,
) (kadm.DescribedConsumerGroups, error) {
	client.describedGroups = append([]string(nil), groups...)

	return client.described, client.describeErr
}

func (client *kadmConsumerProtocolTestClient) FetchManyOffsets(
	_ context.Context,
	groups ...string,
) kadm.FetchOffsetsResponses {
	client.fetchedGroups = append([]string(nil), groups...)

	return client.fetched
}

func (client *kadmConsumerProtocolTestClient) ListStartOffsets(
	_ context.Context,
	topics ...string,
) (kadm.ListedOffsets, error) {
	client.startTopics = append([]string(nil), topics...)

	return client.startOffsets, client.startErr
}

func (client *kadmConsumerProtocolTestClient) ListEndOffsets(
	_ context.Context,
	topics ...string,
) (kadm.ListedOffsets, error) {
	client.endTopics = append([]string(nil), topics...)

	return client.endOffsets, client.endErr
}

type consumerProtocolInspectorTestBackend struct {
	metadataInspectorBackend
	groups      inspectorConsumerProtocolGroups
	err         error
	groupErrors map[string]error
	fn          func(context.Context, ...string) (inspectorConsumerProtocolGroups, error)
}

func (backend *consumerProtocolInspectorTestBackend) ConsumerProtocolLag(
	ctx context.Context,
	requested ...string,
) (inspectorConsumerProtocolGroups, error) {
	if backend.fn != nil {
		return backend.fn(ctx, requested...)
	}
	result := make(inspectorConsumerProtocolGroups)
	for _, group := range requested {
		if err := backend.groupErrors[group]; err != nil {
			return nil, err
		}
		state, exists := backend.groups[group]
		if !exists {
			return nil, ErrInvalidInspectionResponse
		}
		result[group] = state
	}
	return result, backend.err
}
