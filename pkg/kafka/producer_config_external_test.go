package kafka_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
)

func TestProducerConfigValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config  kafka.ProducerConfig
		wantErr error
	}{
		"valid": {
			config: kafka.ProducerConfig{
				Brokers:  []string{"kafka.internal:9093"},
				ClientID: "track",
			},
		},
		"missing broker": {
			config: kafka.ProducerConfig{
				ClientID: "track",
			},
			wantErr: kafka.ErrBrokersRequired,
		},
		"unbounded buffering": {
			config: kafka.ProducerConfig{
				Brokers:            []string{"kafka.internal:9093"},
				ClientID:           "track",
				MaxBufferedRecords: 1_000_001,
			},
			wantErr: kafka.ErrInvalidProducerConfig,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := test.config.Validate()
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
