package rabbitmq

import (
	"errors"
	"testing"
)

func BenchmarkReconnectRecovery(b *testing.B) {
	original := dialAMQP
	b.Cleanup(func() { dialAMQP = original })
	attempts := 0
	dialAMQP = func(string) (amqpConnection, error) {
		attempts++
		if attempts%2 == 1 {
			return nil, errors.New("unavailable")
		}
		return &fakeAMQPConnection{}, nil
	}
	config := ReconnectConfig{MaxRetries: 2}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := dialWithRetry("amqp://rabbit", config); err != nil {
			b.Fatal(err)
		}
	}
}
