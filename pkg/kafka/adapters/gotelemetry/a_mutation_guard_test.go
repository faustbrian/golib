package gotelemetry

import (
	"fmt"
	"testing"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

func TestObservationDiagnosticsDistinguishZeroFromOne(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"kafka.record.count":                int64(1),
		"kafka.partition.count":             int64(1),
		"kafka.broker.count":                int64(1),
		"kafka.topic.count":                 int64(1),
		"kafka.consumer_group.count":        int64(1),
		"kafka.consumer_group.member.count": int64(1),
		"kafka.record.processed_count":      int64(1),
		"kafka.record.committed_count":      int64(1),
		"kafka.record.size":                 int64(1),
	}
	positive := attributeMap(appendObservationDiagnostics(nil, kafka.Observation{
		RecordCount:      1,
		PartitionCount:   1,
		BrokerCount:      1,
		TopicCount:       1,
		GroupCount:       1,
		GroupMemberCount: 1,
		ProcessedCount:   1,
		CommittedCount:   1,
		RecordBytes:      1,
	}))
	if len(positive) != len(want) {
		t.Fatalf("positive diagnostic count = %d, want %d: %#v", len(positive), len(want), positive)
	}
	for key, value := range want {
		if positive[key] != value {
			t.Fatalf("positive diagnostic %q = %#v, want %#v", key, positive[key], value)
		}
	}

	zero := attributeMap(appendObservationDiagnostics(nil, kafka.Observation{}))
	for key := range want {
		if _, exists := zero[key]; exists {
			t.Fatalf("zero observation emitted %q", key)
		}
	}
}

func TestAttributePolicyAcceptsExactAllowlistAndTopicAlphabetBoundaries(
	t *testing.T,
) {
	t.Parallel()

	values := make([]string, maxAllowedValues)
	for index := range values {
		values[index] = fmt.Sprintf("client-%d", index)
	}
	if err := (AttributePolicy{AllowedClientIDs: values}).Validate(); err != nil {
		t.Fatalf("exact allowlist size error = %v", err)
	}

	for _, topic := range []string{"a", "z", "A", "Z", "0", "9"} {
		if err := (AttributePolicy{AllowedTopics: []string{topic}}).Validate(); err != nil {
			t.Fatalf("topic boundary %q error = %v", topic, err)
		}
	}
}
