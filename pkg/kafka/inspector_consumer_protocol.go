package kafka

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
)

// ConsumerProtocolMemberType identifies the KIP-848 member representation
// reported by Kafka. Unknown is retained because version-zero describe
// responses do not expose the member type.
type ConsumerProtocolMemberType int8

const (
	// ConsumerProtocolMemberTypeUnknown means the response version omitted the
	// member type.
	ConsumerProtocolMemberTypeUnknown ConsumerProtocolMemberType = -1
	// ConsumerProtocolMemberTypeClassic identifies a classic member migrating
	// within a KIP-848 consumer group.
	ConsumerProtocolMemberTypeClassic ConsumerProtocolMemberType = 0
	// ConsumerProtocolMemberTypeConsumer identifies a KIP-848 consumer member.
	ConsumerProtocolMemberTypeConsumer ConsumerProtocolMemberType = 1
)

// ConsumerProtocolGroupMemberState is bounded, copied KIP-848 consumer-group
// member state. Assignments are the member's current ownership; target
// assignments are the broker-selected state toward which it is reconciling.
type ConsumerProtocolGroupMemberState struct {
	MemberID string
	// InstanceID is the static member identity when InstanceIDVisible is true.
	InstanceID        string
	InstanceIDVisible bool
	// RackID is the member rack identity when RackIDVisible is true.
	RackID        string
	RackIDVisible bool
	MemberEpoch   int32
	MemberType    ConsumerProtocolMemberType
	ClientID      string
	ClientHost    string
	// SubscribedTopics is an owned, sorted explicit topic subscription.
	SubscribedTopics []string
	// SubscribedTopicRegex is the broker-side subscription expression when
	// SubscribedTopicRegexVisible is true. Kafka can expose an empty expression
	// for an explicit topic subscription, so visibility must be checked first.
	SubscribedTopicRegex        string
	SubscribedTopicRegexVisible bool
	// Assignments and TargetAssignments are owned and sorted by topic and
	// partition. They remain distinct while a member reconciles.
	Assignments       []TopicPartition
	TargetAssignments []TopicPartition
}

// ConsumerProtocolGroupState is current KIP-848 group state and lag. Epoch is
// the group metadata epoch; AssignmentEpoch identifies the target assignment.
// Members may temporarily have lower epochs and current assignments while
// independently reconciling toward their target assignments.
type ConsumerProtocolGroupState struct {
	Group         string
	CoordinatorID int32
	// State retains Kafka's title-cased wire value.
	State string
	Epoch int32
	// AssignmentEpoch identifies the target assignment computed for Epoch.
	AssignmentEpoch int32
	Assignor        string
	// Members are owned and sorted by member ID.
	Members []ConsumerProtocolGroupMemberState
	// Partitions are owned and sorted by topic and partition.
	Partitions []ConsumerGroupPartitionLag
}

// ConsumerProtocolGroupInspectionResult is one input-ordered KIP-848 group
// inspection outcome. State is populated only when Err is nil. Err retains the
// target-specific broker or policy failure, and Category provides its stable
// package classification.
type ConsumerProtocolGroupInspectionResult struct {
	Group string
	// State is populated only when Err is nil.
	State ConsumerProtocolGroupState
	// Category is zero on success and classifies Err on failure.
	Category ErrorCategory
	Err      error
}

type inspectorConsumerProtocolMember struct {
	memberID             string
	instanceID           *string
	rackID               *string
	memberEpoch          int32
	memberType           int8
	clientID             string
	clientHost           string
	subscribedTopics     []string
	subscribedTopicRegex *string
	assignments          map[string][]int32
	targetAssignments    map[string][]int32
}

type inspectorConsumerProtocolGroup struct {
	group           string
	coordinatorID   int32
	state           string
	epoch           int32
	assignmentEpoch int32
	assignor        string
	members         []inspectorConsumerProtocolMember
	partitions      []ConsumerGroupPartitionLag
	describeErr     error
	fetchErr        error
}

func (group inspectorConsumerProtocolGroup) err() error {
	if group.describeErr != nil {
		return group.describeErr
	}

	return group.fetchErr
}

type inspectorConsumerProtocolGroups map[string]inspectorConsumerProtocolGroup

func (groups inspectorConsumerProtocolGroups) sorted() []inspectorConsumerProtocolGroup {
	result := make([]inspectorConsumerProtocolGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	slices.SortFunc(result, func(left, right inspectorConsumerProtocolGroup) int {
		return cmp.Compare(left.group, right.group)
	})

	return result
}

func (groups inspectorConsumerProtocolGroups) err() error {
	for _, group := range groups.sorted() {
		if err := group.err(); err != nil {
			return err
		}
	}

	return nil
}

type consumerProtocolInspectorBackend interface {
	ConsumerProtocolLag(
		context.Context,
		...string,
	) (inspectorConsumerProtocolGroups, error)
}

type kadmConsumerProtocolClient interface {
	DescribeConsumerGroups(
		context.Context,
		...string,
	) (kadm.DescribedConsumerGroups, error)
	FetchManyOffsets(context.Context, ...string) kadm.FetchOffsetsResponses
	ListStartOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
	ListEndOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
}

func (backend *franzInspectorBackend) ConsumerProtocolLag(
	ctx context.Context,
	groups ...string,
) (inspectorConsumerProtocolGroups, error) {
	client := backend.consumerProtocolGroups
	if client == nil {
		return nil, ErrInvalidInspectionResponse
	}
	described, err := client.DescribeConsumerGroups(ctx, groups...)
	if err != nil {
		return nil, err
	}
	if err := validateConsumerProtocolDescriptionTargets(groups, described); err != nil {
		return nil, err
	}
	translated, err := translateConsumerProtocolGroups(
		described,
		backend.maxGroupMembers,
		backend.maxMetadataPartitions,
	)
	if err != nil {
		return nil, err
	}
	active := make([]string, 0, len(translated))
	for _, group := range translated.sorted() {
		if group.describeErr == nil {
			active = append(active, group.group)
		}
	}
	if len(active) == 0 {
		return translated, nil
	}

	fetched := client.FetchManyOffsets(kadm.RequireStable(ctx), active...)
	if len(fetched) != len(active) {
		return nil, ErrInvalidInspectionResponse
	}
	for _, groupName := range active {
		response, exists := fetched[groupName]
		if !exists || response.Group != groupName {
			return nil, ErrInvalidInspectionResponse
		}
		if response.Err != nil {
			group := translated[groupName]
			group.fetchErr = response.Err
			translated[groupName] = group

			continue
		}
		if err := validateConsumerProtocolFetchedOffsets(response.Fetched); err != nil {
			return nil, err
		}
	}

	topics := consumerProtocolLagTopics(translated, fetched)
	if len(topics) == 0 {
		return translated, nil
	}
	startOffsets, startErr := client.ListStartOffsets(ctx, topics...)
	endOffsets, endErr := client.ListEndOffsets(ctx, topics...)
	if err := errors.Join(startErr, endErr); err != nil {
		return nil, err
	}
	if err := validateConsumerProtocolListedOffsets(
		topics,
		startOffsets,
		endOffsets,
		backend.maxMetadataPartitions,
	); err != nil {
		return nil, err
	}
	metadataEntries := consumerProtocolMetadataEntries(translated)
	for _, groupName := range active {
		group := translated[groupName]
		if group.fetchErr != nil {
			continue
		}
		partitions, err := calculateConsumerProtocolGroupLag(
			group,
			fetched[groupName].Fetched,
			startOffsets,
			endOffsets,
			&metadataEntries,
			backend.maxMetadataPartitions,
		)
		if err != nil {
			return nil, err
		}
		group.partitions = partitions
		translated[groupName] = group
	}

	return translated, nil
}

func validateConsumerProtocolFetchedOffsets(offsets kadm.OffsetResponses) error {
	for topic, partitions := range offsets {
		if !validKafkaTopicName(topic, 249) {
			return ErrInvalidInspectionResponse
		}
		for partition, offset := range partitions {
			if partition < 0 ||
				offset.Topic != topic ||
				offset.Partition != partition ||
				offset.At < -1 {
				return ErrInvalidInspectionResponse
			}
		}
	}

	return nil
}

func validateConsumerProtocolListedOffsets(
	topics []string,
	startOffsets kadm.ListedOffsets,
	endOffsets kadm.ListedOffsets,
	maximumPartitions int,
) error {
	if len(startOffsets) != len(topics) || len(endOffsets) != len(topics) {
		return ErrInvalidInspectionResponse
	}
	requested := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		requested[topic] = struct{}{}
	}
	remainingPartitions := maximumPartitions
	tooLarge := false
	for topic, starts := range startOffsets {
		if _, exists := requested[topic]; !exists {
			return ErrInvalidInspectionResponse
		}
		ends, exists := endOffsets[topic]
		if !exists || len(starts) != len(ends) {
			return ErrInvalidInspectionResponse
		}
		if len(starts) > remainingPartitions {
			tooLarge = true
		} else {
			remainingPartitions -= len(starts)
		}
		for partition, start := range starts {
			end, exists := ends[partition]
			if !exists || partition < 0 ||
				start.Topic != topic ||
				start.Partition != partition ||
				end.Topic != topic ||
				end.Partition != partition {
				return ErrInvalidInspectionResponse
			}
		}
	}
	if tooLarge {
		return ErrInspectionResponseTooLarge
	}

	return nil
}

func consumerProtocolMetadataEntries(
	groups inspectorConsumerProtocolGroups,
) int {
	entries := 0
	for _, group := range groups {
		for _, member := range group.members {
			entries += len(member.subscribedTopics)
			for _, assignments := range []map[string][]int32{
				member.assignments,
				member.targetAssignments,
			} {
				entries += len(assignments)
				for _, partitions := range assignments {
					entries += len(partitions)
				}
			}
		}
	}

	return entries
}

func validateConsumerProtocolDescriptionTargets(
	requested []string,
	described kadm.DescribedConsumerGroups,
) error {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, group := range requested {
		requestedSet[group] = struct{}{}
	}
	if len(described) != len(requestedSet) {
		return ErrInvalidInspectionResponse
	}
	for groupName, group := range described {
		if _, requested := requestedSet[groupName]; !requested ||
			group.Group != groupName {
			return ErrInvalidInspectionResponse
		}
	}

	return nil
}

func translateConsumerProtocolGroups(
	described kadm.DescribedConsumerGroups,
	maxMembers int,
	maxMetadataEntries int,
) (inspectorConsumerProtocolGroups, error) {
	if _, err := consumerProtocolGroupMemberCount(described, maxMembers); err != nil {
		return nil, err
	}
	metadataEntries := 0
	borrowed := make(inspectorConsumerProtocolGroups, len(described))
	for _, groupName := range consumerProtocolDescribedGroupNames(described) {
		group := described[groupName]
		if group.Err != nil {
			borrowed[groupName] = inspectorConsumerProtocolGroup{
				group: group.Group, describeErr: group.Err,
			}

			continue
		}
		for _, member := range group.Members {
			if err := consumeConsumerProtocolMemberBudget(
				&metadataEntries,
				maxMetadataEntries,
				member,
			); err != nil {
				return nil, err
			}
		}
		members := make([]inspectorConsumerProtocolMember, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, inspectorConsumerProtocolMember{
				memberID:             member.MemberID,
				instanceID:           member.InstanceID,
				rackID:               member.RackID,
				memberEpoch:          member.MemberEpoch,
				memberType:           member.MemberType,
				clientID:             member.ClientID,
				clientHost:           member.ClientHost,
				subscribedTopics:     member.SubscribedTopics,
				subscribedTopicRegex: member.SubscribedTopicRegex,
				assignments:          translateTopicsSet(member.Assignment),
				targetAssignments:    translateTopicsSet(member.TargetAssignment),
			})
		}
		borrowed[groupName] = inspectorConsumerProtocolGroup{
			group:           group.Group,
			coordinatorID:   group.Coordinator.NodeID,
			state:           group.State,
			epoch:           group.Epoch,
			assignmentEpoch: group.AssignmentEpoch,
			assignor:        group.AssignorName,
			members:         members,
			describeErr:     group.Err,
		}
	}
	requested := make([]string, 0, len(described))
	for groupName := range described {
		requested = append(requested, groupName)
	}
	if err := validateConsumerProtocolGroupsWithLimits(
		requested,
		borrowed,
		maxMembers,
		maxMetadataEntries,
	); err != nil {
		return nil, err
	}

	return cloneConsumerProtocolGroups(borrowed), nil
}

func consumerProtocolGroupMemberCount(
	described kadm.DescribedConsumerGroups,
	maxMembers int,
) (int, error) {
	memberCount := 0
	for _, groupName := range consumerProtocolDescribedGroupNames(described) {
		group := described[groupName]
		if group.Err != nil {
			continue
		}
		if len(group.Members) > maxMembers-memberCount {
			return 0, ErrInspectionResponseTooLarge
		}
		memberCount += len(group.Members)
	}

	return memberCount, nil
}

func consumerProtocolDescribedGroupNames(
	described kadm.DescribedConsumerGroups,
) []string {
	groups := make([]string, 0, len(described))
	for group := range described {
		groups = append(groups, group)
	}
	slices.Sort(groups)

	return groups
}

func consumeConsumerProtocolMemberBudget(
	used *int,
	maximum int,
	member kadm.ConsumerGroupMember,
) error {
	entries := len(member.SubscribedTopics)
	for _, assignments := range []kadm.TopicsSet{
		member.Assignment,
		member.TargetAssignment,
	} {
		entries += len(assignments)
		for _, partitions := range assignments {
			entries += len(partitions)
		}
	}
	if *used > maximum || entries > maximum-*used {
		return ErrInspectionResponseTooLarge
	}
	*used += entries

	return nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := strings.Clone(*value)

	return &cloned
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.Clone(value)
	}

	return result
}

func translateTopicsSet(source kadm.TopicsSet) map[string][]int32 {
	result := make(map[string][]int32, len(source))
	for topic, partitions := range source {
		copied := make([]int32, 0, len(partitions))
		for partition := range partitions {
			copied = append(copied, partition)
		}
		result[topic] = copied
	}

	return result
}

func cloneConsumerProtocolGroups(
	source inspectorConsumerProtocolGroups,
) inspectorConsumerProtocolGroups {
	result := make(inspectorConsumerProtocolGroups, len(source))
	for groupName, group := range source {
		cloned := inspectorConsumerProtocolGroup{
			group:           strings.Clone(group.group),
			coordinatorID:   group.coordinatorID,
			state:           strings.Clone(group.state),
			epoch:           group.epoch,
			assignmentEpoch: group.assignmentEpoch,
			assignor:        strings.Clone(group.assignor),
			describeErr:     group.describeErr,
			fetchErr:        group.fetchErr,
		}
		cloned.members = make([]inspectorConsumerProtocolMember, 0, len(group.members))
		for _, member := range group.members {
			cloned.members = append(cloned.members, inspectorConsumerProtocolMember{
				memberID:             strings.Clone(member.memberID),
				instanceID:           cloneStringPointer(member.instanceID),
				rackID:               cloneStringPointer(member.rackID),
				memberEpoch:          member.memberEpoch,
				memberType:           member.memberType,
				clientID:             strings.Clone(member.clientID),
				clientHost:           strings.Clone(member.clientHost),
				subscribedTopics:     cloneStrings(member.subscribedTopics),
				subscribedTopicRegex: cloneStringPointer(member.subscribedTopicRegex),
				assignments:          clonePartitionAssignments(member.assignments),
				targetAssignments:    clonePartitionAssignments(member.targetAssignments),
			})
		}
		result[strings.Clone(groupName)] = cloned
	}

	return result
}

func clonePartitionAssignments(source map[string][]int32) map[string][]int32 {
	result := make(map[string][]int32, len(source))
	for topic, partitions := range source {
		result[strings.Clone(topic)] = append([]int32(nil), partitions...)
	}

	return result
}

func consumerProtocolLagTopics(
	groups inspectorConsumerProtocolGroups,
	fetched kadm.FetchOffsetsResponses,
) []string {
	topics := make(map[string]struct{})
	for _, group := range groups.sorted() {
		if group.describeErr != nil || group.fetchErr != nil {
			continue
		}
		for _, member := range group.members {
			for _, topic := range member.subscribedTopics {
				topics[topic] = struct{}{}
			}
			for topic := range member.assignments {
				topics[topic] = struct{}{}
			}
			for topic := range member.targetAssignments {
				topics[topic] = struct{}{}
			}
		}
		for topic := range fetched[group.group].Fetched {
			topics[topic] = struct{}{}
		}
	}
	result := make([]string, 0, len(topics))
	for topic := range topics {
		result = append(result, topic)
	}
	slices.Sort(result)

	return result
}

func calculateConsumerProtocolGroupLag(
	group inspectorConsumerProtocolGroup,
	committed kadm.OffsetResponses,
	startOffsets kadm.ListedOffsets,
	endOffsets kadm.ListedOffsets,
	metadataEntries *int,
	maximumMetadataEntries int,
) ([]ConsumerGroupPartitionLag, error) {
	partitions := make(map[TopicPartition]struct{})
	addPartition := func(partition TopicPartition) error {
		if _, exists := partitions[partition]; exists {
			return nil
		}
		if *metadataEntries >= maximumMetadataEntries {
			return ErrInspectionResponseTooLarge
		}
		*metadataEntries++
		partitions[partition] = struct{}{}

		return nil
	}
	for _, member := range group.members {
		for topic, assigned := range member.assignments {
			for _, partition := range assigned {
				if err := addPartition(TopicPartition{
					Topic: topic, Partition: partition,
				}); err != nil {
					return nil, err
				}
			}
		}
		for topic, assigned := range member.targetAssignments {
			for _, partition := range assigned {
				if err := addPartition(TopicPartition{
					Topic: topic, Partition: partition,
				}); err != nil {
					return nil, err
				}
			}
		}
		for _, topic := range member.subscribedTopics {
			for partition := range endOffsets[topic] {
				if err := addPartition(TopicPartition{
					Topic: topic, Partition: partition,
				}); err != nil {
					return nil, err
				}
			}
		}
	}
	for topic, offsets := range committed {
		for partition := range offsets {
			if err := addPartition(TopicPartition{
				Topic: topic, Partition: partition,
			}); err != nil {
				return nil, err
			}
		}
	}

	ordered := make([]TopicPartition, 0, len(partitions))
	for partition := range partitions {
		ordered = append(ordered, partition)
	}
	slices.SortFunc(ordered, compareTopicPartition)
	result := make([]ConsumerGroupPartitionLag, 0, len(ordered))
	for _, partition := range ordered {
		committedOffset := int64(-1)
		if offset, exists := committed[partition.Topic][partition.Partition]; exists {
			if offset.Err != nil {
				return nil, offset.Err
			}
			committedOffset = offset.At
		}
		start, startExists := startOffsets[partition.Topic][partition.Partition]
		end, endExists := endOffsets[partition.Topic][partition.Partition]
		if !startExists || !endExists {
			return nil, ErrInvalidInspectionResponse
		}
		if start.Err != nil {
			return nil, start.Err
		}
		if end.Err != nil {
			return nil, end.Err
		}
		lag := end.Offset - start.Offset
		if committedOffset >= 0 {
			lag = end.Offset - committedOffset
		}
		result = append(result, ConsumerGroupPartitionLag{
			Topic:           strings.Clone(partition.Topic),
			Partition:       partition.Partition,
			CommittedOffset: committedOffset,
			StartOffset:     start.Offset,
			EndOffset:       end.Offset,
			Lag:             max(lag, 0),
		})
	}

	return result, nil
}

// ConsumerProtocolGroupLag requests sorted, bounded KIP-848 consumer-group
// state and stable committed-offset lag for an explicit group set. It is
// separate from ConsumerGroupLag because Kafka's classic and consumer group
// protocols expose materially different assignment and fencing state.
func (inspector *Inspector) ConsumerProtocolGroupLag(
	ctx context.Context,
	groups ...string,
) (result []ConsumerProtocolGroupState, resultErr error) {
	if err := validateInspectionTargets(groups, 255); err != nil {
		return nil, err
	}
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	startedAt := inspector.observationStart()
	defer func() {
		inspector.observeInspection(
			ctx,
			ObservationInspectorConsumerGroups,
			startedAt,
			resultErr,
			func(observation *Observation) {
				observation.GroupCount = len(groups)
				for _, group := range result {
					observation.GroupMemberCount += len(group.Members)
					observation.PartitionCount += len(group.Partitions)
				}
			},
		)
	}()

	backend, supported := inspector.admin.(consumerProtocolInspectorBackend)
	if !supported {
		return nil, ErrInvalidInspectionResponse
	}
	states, err := backend.ConsumerProtocolLag(requestCtx, groups...)
	if err != nil {
		return nil, inspectionRequestError(requestCtx, err)
	}
	if cause := context.Cause(requestCtx); cause != nil {
		return nil, cause
	}
	if err := states.err(); err != nil {
		return nil, err
	}
	if err := inspector.validateConsumerProtocolGroups(groups, states); err != nil {
		return nil, err
	}

	result = make([]ConsumerProtocolGroupState, 0, len(states))
	for _, state := range states.sorted() {
		group := ConsumerProtocolGroupState{
			Group:           strings.Clone(state.group),
			CoordinatorID:   state.coordinatorID,
			State:           strings.Clone(state.state),
			Epoch:           state.epoch,
			AssignmentEpoch: state.assignmentEpoch,
			Assignor:        strings.Clone(state.assignor),
			Members:         make([]ConsumerProtocolGroupMemberState, 0, len(state.members)),
			Partitions:      append([]ConsumerGroupPartitionLag(nil), state.partitions...),
		}
		for _, member := range state.members {
			publicMember := ConsumerProtocolGroupMemberState{
				MemberID:          strings.Clone(member.memberID),
				MemberEpoch:       member.memberEpoch,
				MemberType:        ConsumerProtocolMemberType(member.memberType),
				ClientID:          strings.Clone(member.clientID),
				ClientHost:        strings.Clone(member.clientHost),
				SubscribedTopics:  cloneStrings(member.subscribedTopics),
				Assignments:       flattenTopicPartitions(member.assignments),
				TargetAssignments: flattenTopicPartitions(member.targetAssignments),
			}
			if member.instanceID != nil {
				publicMember.InstanceID = strings.Clone(*member.instanceID)
				publicMember.InstanceIDVisible = true
			}
			if member.rackID != nil {
				publicMember.RackID = strings.Clone(*member.rackID)
				publicMember.RackIDVisible = true
			}
			if member.subscribedTopicRegex != nil {
				publicMember.SubscribedTopicRegex = strings.Clone(
					*member.subscribedTopicRegex,
				)
				publicMember.SubscribedTopicRegexVisible = true
			}
			slices.Sort(publicMember.SubscribedTopics)
			group.Members = append(group.Members, publicMember)
		}
		slices.SortFunc(group.Members, func(
			left ConsumerProtocolGroupMemberState,
			right ConsumerProtocolGroupMemberState,
		) int {
			return cmp.Compare(left.MemberID, right.MemberID)
		})
		slices.SortFunc(group.Partitions, func(
			left ConsumerGroupPartitionLag,
			right ConsumerGroupPartitionLag,
		) int {
			return compareTopicPartition(
				TopicPartition{Topic: left.Topic, Partition: left.Partition},
				TopicPartition{Topic: right.Topic, Partition: right.Partition},
			)
		})
		result = append(result, group)
	}

	return result, nil
}

// InspectConsumerProtocolGroups returns one input-ordered result for every
// explicit KIP-848 consumer group. Target failures do not discard independent
// successes. The returned error is ErrInspectionTargetsFailed when any result
// failed. Requests share one deadline and never exceed
// MaxConcurrentInspections.
func (inspector *Inspector) InspectConsumerProtocolGroups(
	ctx context.Context,
	groups ...string,
) ([]ConsumerProtocolGroupInspectionResult, error) {
	if err := validateInspectionTargets(groups, 255); err != nil {
		return nil, err
	}
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	outcomes := runInspectionTargets(
		requestCtx,
		groups,
		inspector.inspectionConcurrencyLimit(),
		func(ctx context.Context, group string) (ConsumerProtocolGroupState, error) {
			states, err := inspector.ConsumerProtocolGroupLag(ctx, group)
			if err != nil {
				return ConsumerProtocolGroupState{}, err
			}

			return states[0], nil
		},
	)
	results := make([]ConsumerProtocolGroupInspectionResult, len(groups))
	failed := false
	for index, group := range groups {
		results[index] = ConsumerProtocolGroupInspectionResult{
			Group: strings.Clone(group),
			State: outcomes[index].state,
			Err:   outcomes[index].err,
		}
		if outcomes[index].err != nil {
			results[index].Category = classifyError(outcomes[index].err)
			failed = true
		}
	}
	if failed {
		return results, ErrInspectionTargetsFailed
	}

	return results, nil
}

func flattenTopicPartitions(assignments map[string][]int32) []TopicPartition {
	result := make([]TopicPartition, 0)
	for topic, partitions := range assignments {
		for _, partition := range partitions {
			result = append(result, TopicPartition{
				Topic: strings.Clone(topic), Partition: partition,
			})
		}
	}
	slices.SortFunc(result, compareTopicPartition)

	return result
}

func (inspector *Inspector) validateConsumerProtocolGroups(
	requested []string,
	groups inspectorConsumerProtocolGroups,
) error {
	return validateConsumerProtocolGroupsWithLimits(
		requested,
		groups,
		inspector.groupMemberLimit(),
		inspector.metadataPartitionLimit(),
	)
}

func validateConsumerProtocolGroupsWithLimits(
	requested []string,
	groups inspectorConsumerProtocolGroups,
	memberLimit int,
	metadataLimit int,
) error {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, group := range requested {
		requestedSet[group] = struct{}{}
	}
	if len(groups) != len(requestedSet) {
		return ErrInvalidInspectionResponse
	}

	memberCount := 0
	metadataEntries := 0
	for _, groupName := range requested {
		group, exists := groups[groupName]
		if !exists || group.group != groupName {
			return ErrInvalidInspectionResponse
		}
		if group.err() != nil {
			continue
		}
		if group.coordinatorID < 0 ||
			!validConsumerProtocolGroupState(group.state) ||
			group.epoch < 0 ||
			group.assignmentEpoch < 0 ||
			group.assignmentEpoch > group.epoch ||
			group.assignor != strings.TrimSpace(group.assignor) ||
			!validKafkaText(group.assignor, 255) ||
			(len(group.members) > 0 && group.assignor == "") {
			return ErrInvalidInspectionResponse
		}

		memberCount += len(group.members)
		if memberCount > memberLimit {
			return ErrInspectionResponseTooLarge
		}
		memberIDs := make(map[string]struct{}, len(group.members))
		instanceIDs := make(map[string]struct{}, len(group.members))
		currentOwners := make(map[TopicPartition]struct{})
		targetOwners := make(map[TopicPartition]struct{})
		for _, member := range group.members {
			if member.memberID == "" ||
				member.memberID != strings.TrimSpace(member.memberID) ||
				!validKafkaText(member.memberID, 1_024) ||
				member.memberEpoch < 0 ||
				member.memberEpoch > group.epoch ||
				member.memberType < int8(ConsumerProtocolMemberTypeUnknown) ||
				member.memberType > int8(ConsumerProtocolMemberTypeConsumer) ||
				member.clientID != strings.TrimSpace(member.clientID) ||
				!validKafkaText(member.clientID, 255) ||
				member.clientHost != strings.TrimSpace(member.clientHost) ||
				!validKafkaText(member.clientHost, 255) {
				return ErrInvalidInspectionResponse
			}
			if _, duplicate := memberIDs[member.memberID]; duplicate {
				return ErrInvalidInspectionResponse
			}
			memberIDs[member.memberID] = struct{}{}
			if err := validateOptionalConsumerProtocolText(
				member.instanceID,
				255,
			); err != nil {
				return err
			}
			if member.instanceID != nil {
				if _, duplicate := instanceIDs[*member.instanceID]; duplicate {
					return ErrInvalidInspectionResponse
				}
				instanceIDs[*member.instanceID] = struct{}{}
			}
			if err := validateOptionalConsumerProtocolText(
				member.rackID,
				255,
			); err != nil {
				return err
			}
			if member.subscribedTopicRegex != nil &&
				(!validKafkaText(*member.subscribedTopicRegex, 1_024) ||
					*member.subscribedTopicRegex != strings.TrimSpace(
						*member.subscribedTopicRegex,
					)) {
				return ErrInvalidInspectionResponse
			}
			seenTopics := make(map[string]struct{}, len(member.subscribedTopics))
			for _, topic := range member.subscribedTopics {
				if !validKafkaTopicName(topic, 249) {
					return ErrInvalidInspectionResponse
				}
				if _, duplicate := seenTopics[topic]; duplicate {
					return ErrInvalidInspectionResponse
				}
				seenTopics[topic] = struct{}{}
				metadataEntries++
			}
			if err := validateConsumerProtocolAssignments(
				member.assignments,
				currentOwners,
				&metadataEntries,
				metadataLimit,
			); err != nil {
				return err
			}
			if err := validateConsumerProtocolAssignments(
				member.targetAssignments,
				targetOwners,
				&metadataEntries,
				metadataLimit,
			); err != nil {
				return err
			}
		}
		metadataEntries += len(group.partitions)
		if metadataEntries > metadataLimit {
			return ErrInspectionResponseTooLarge
		}
		seenLag := make(map[TopicPartition]struct{}, len(group.partitions))
		for _, partition := range group.partitions {
			coordinate := TopicPartition{
				Topic: partition.Topic, Partition: partition.Partition,
			}
			if !validKafkaTopicName(partition.Topic, 249) ||
				partition.Partition < 0 ||
				partition.CommittedOffset < -1 ||
				partition.StartOffset < 0 ||
				partition.EndOffset < partition.StartOffset ||
				partition.Lag < 0 {
				return ErrInvalidInspectionResponse
			}
			if _, duplicate := seenLag[coordinate]; duplicate {
				return ErrInvalidInspectionResponse
			}
			seenLag[coordinate] = struct{}{}
			expectedLag := partition.EndOffset - partition.StartOffset
			if partition.CommittedOffset >= 0 {
				expectedLag = partition.EndOffset - partition.CommittedOffset
			}
			if partition.Lag != max(expectedLag, 0) {
				return ErrInvalidInspectionResponse
			}
		}
	}

	return nil
}

func validConsumerProtocolGroupState(state string) bool {
	switch state {
	case "Assigning", "Dead", "Empty", "Reconciling", "Stable":
		return true
	default:
		return false
	}
}

func validateOptionalConsumerProtocolText(value *string, maximum int) error {
	if value == nil {
		return nil
	}
	if *value == "" ||
		*value != strings.TrimSpace(*value) ||
		!validKafkaText(*value, maximum) {
		return ErrInvalidInspectionResponse
	}

	return nil
}

func validateConsumerProtocolAssignments(
	assignments map[string][]int32,
	owners map[TopicPartition]struct{},
	metadataEntries *int,
	maximum int,
) error {
	for topic, partitions := range assignments {
		if !validKafkaTopicName(topic, 249) || len(partitions) == 0 {
			return ErrInvalidInspectionResponse
		}
		if *metadataEntries >= maximum {
			return ErrInspectionResponseTooLarge
		}
		*metadataEntries++
		if len(partitions) > maximum-*metadataEntries {
			return ErrInspectionResponseTooLarge
		}
		*metadataEntries += len(partitions)
		for _, partition := range partitions {
			coordinate := TopicPartition{Topic: topic, Partition: partition}
			if partition < 0 {
				return ErrInvalidInspectionResponse
			}
			if _, duplicate := owners[coordinate]; duplicate {
				return ErrInvalidInspectionResponse
			}
			owners[coordinate] = struct{}{}
		}
	}

	return nil
}
