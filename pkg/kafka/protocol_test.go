package kafka

import (
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
)

func TestProtocolPolicyValidatesMinimumVersion(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"", "3.9", "v3.9", "3.9.7"} {
		version := version
		t.Run("valid_"+version, func(t *testing.T) {
			t.Parallel()

			if err := (ProtocolPolicy{MinimumVersion: version}).Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}

	for _, version := range []string{" ", "3", "99.0", "3.\n9", string([]byte{0xff})} {
		version := version
		t.Run("invalid_"+version, func(t *testing.T) {
			t.Parallel()

			err := (ProtocolPolicy{MinimumVersion: version}).Validate()
			if !errors.Is(err, ErrInvalidProtocolPolicy) {
				t.Fatalf("Validate() error = %v, want %v", err, ErrInvalidProtocolPolicy)
			}
		})
	}
}

func TestKafkaReleaseMinimumComparison(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{
		"2":    false,
		"2.4":  false,
		"2.5":  true,
		"v2.5": true,
		"3.0":  true,
		"x.5":  false,
		"2.x":  false,
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			if got := kafkaReleaseAtLeast(version, 2, 5); got != want {
				t.Fatalf("kafkaReleaseAtLeast(%q) = %t, want %t", version, got, want)
			}
		})
	}
}

func TestClientRolesApplyMinimumProtocolVersion(t *testing.T) {
	t.Parallel()

	const version = "3.9"
	assertMinimum := func(t *testing.T, client *kgo.Client) {
		t.Helper()

		got, ok := client.OptValue(kgo.MinVersions).(*kversion.Versions)
		want := kversion.FromString(version)
		if !ok || !got.Equal(want) {
			t.Fatalf("MinVersions option = %#v, want %s", got, version)
		}
	}

	t.Run("producer", func(t *testing.T) {
		t.Parallel()

		var client *kgo.Client
		producer, err := newProducer(ProducerConfig{
			Brokers:       []string{"broker.internal:9092"},
			ClientID:      "track-producer",
			AllowedTopics: []string{"events"},
			Protocol:      ProtocolPolicy{MinimumVersion: version},
		}, func(options ...kgo.Opt) (*kgo.Client, error) {
			var clientErr error
			client, clientErr = kgo.NewClient(options...)

			return client, clientErr
		})
		if err != nil {
			t.Fatalf("newProducer() error = %v", err)
		}
		defer closeProducerForTest(t, producer)
		assertMinimum(t, client)
	})

	t.Run("consumer", func(t *testing.T) {
		t.Parallel()

		config := validConsumerConfig()
		config.Protocol = ProtocolPolicy{MinimumVersion: version}
		var client *kgo.Client
		consumer, err := newConsumer(config, func(options ...kgo.Opt) (*kgo.Client, error) {
			var clientErr error
			client, clientErr = kgo.NewClient(options...)

			return client, clientErr
		})
		if err != nil {
			t.Fatalf("newConsumer() error = %v", err)
		}
		defer closeConsumerForTest(t, consumer)
		assertMinimum(t, client)
	})

	t.Run("replay", func(t *testing.T) {
		t.Parallel()

		config := validReplayConfig()
		config.Protocol = ProtocolPolicy{MinimumVersion: version}
		var client *kgo.Client
		reader, err := newReplayReader(config, func(options ...kgo.Opt) (*kgo.Client, error) {
			var clientErr error
			client, clientErr = kgo.NewClient(options...)

			return client, clientErr
		})
		if err != nil {
			t.Fatalf("newReplayReader() error = %v", err)
		}
		defer closeReplayReaderForTest(t, reader)
		assertMinimum(t, client)
	})

	t.Run("inspector", func(t *testing.T) {
		t.Parallel()

		var client *kgo.Client
		inspector, err := newInspector(InspectorConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "track-inspector",
			Protocol: ProtocolPolicy{MinimumVersion: version},
		}, func(options ...kgo.Opt) (*kgo.Client, error) {
			var clientErr error
			client, clientErr = kgo.NewClient(options...)

			return client, clientErr
		}, func(*kgo.Client, InspectorConfig) inspectorBackend {
			return &recordingInspectorBackend{}
		})
		if err != nil {
			t.Fatalf("newInspector() error = %v", err)
		}
		defer inspector.Close()
		assertMinimum(t, client)
	})
}

func TestClientRolesRejectInvalidProtocolPolicy(t *testing.T) {
	t.Parallel()

	policy := ProtocolPolicy{MinimumVersion: "unknown"}
	producerConfig := ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track-producer",
		AllowedTopics: []string{"events"},
		Protocol:      policy,
	}
	consumerConfig := validConsumerConfig()
	consumerConfig.Protocol = policy
	replayConfig := validReplayConfig()
	replayConfig.Protocol = policy
	inspectorConfig := InspectorConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track-inspector",
		Protocol: policy,
	}

	tests := map[string]func() error{
		"producer": func() error {
			_, err := normalizeProducerConfig(producerConfig)

			return err
		},
		"consumer": func() error {
			_, err := normalizeConsumerConfig(consumerConfig)

			return err
		},
		"replay": func() error {
			_, err := normalizeReplayConfig(replayConfig)

			return err
		},
		"inspector": func() error {
			_, err := normalizeInspectorConfig(inspectorConfig)

			return err
		},
	}
	for name, run := range tests {
		run := run
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := run(); !errors.Is(err, ErrInvalidProtocolPolicy) {
				t.Fatalf("normalize error = %v, want %v", err, ErrInvalidProtocolPolicy)
			}
		})
	}
}
