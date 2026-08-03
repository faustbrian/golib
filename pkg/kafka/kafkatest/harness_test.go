package kafkatest

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
)

func TestCommittedOffsetAssertionWaitsForBrokerVisibility(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	harness := BrokerHarness{
		CommittedOffset: func(
			context.Context,
			string,
			string,
			int32,
		) (int64, error) {
			if calls.Add(1) < 3 {
				return -1, nil
			}

			return 1, nil
		},
	}
	assertConformanceCommittedOffset(t, harness, "group", "topic", 0, 1)
	if calls.Load() != 3 {
		t.Fatalf("committed-offset lookup count = %d", calls.Load())
	}
}

func TestBrokerHarnessRejectsIncompleteOrUnsafeFixtures(t *testing.T) {
	t.Parallel()

	valid := BrokerHarness{
		Brokers: []string{"broker.example:9093"},
		NewTopic: func(*testing.T, int) string {
			return "conformance"
		},
		ReadRecords: func(context.Context, ReadRequest) ([]kafka.ConsumedRecord, error) {
			return nil, nil
		},
		CommittedOffset: func(context.Context, string, string, int32) (int64, error) {
			return -1, nil
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid harness: %v", err)
	}

	tests := map[string]func(*BrokerHarness){
		"brokers required": func(harness *BrokerHarness) {
			harness.Brokers = nil
		},
		"blank broker rejected": func(harness *BrokerHarness) {
			harness.Brokers = []string{" "}
		},
		"duplicate broker rejected": func(harness *BrokerHarness) {
			harness.Brokers = []string{"broker.example:9093", "broker.example:9093"}
		},
		"topic factory required": func(harness *BrokerHarness) {
			harness.NewTopic = nil
		},
		"direct reader required": func(harness *BrokerHarness) {
			harness.ReadRecords = nil
		},
		"committed offset lookup required": func(harness *BrokerHarness) {
			harness.CommittedOffset = nil
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			harness := valid
			harness.Brokers = append([]string(nil), valid.Brokers...)
			mutate(&harness)
			if err := harness.Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
}

func TestAuthenticationProviderHarnessRequiresEveryProvider(t *testing.T) {
	t.Parallel()

	if err := (AuthenticationProviderHarness{}).Validate(); err == nil {
		t.Fatal("Validate() succeeded")
	}
}
