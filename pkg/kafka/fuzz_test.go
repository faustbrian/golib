package kafka

import (
	"context"
	"strings"
	"testing"
	"time"
)

func FuzzConsumerConfig(f *testing.F) {
	f.Add(
		"events",
		"projection-v1",
		uint16(100),
		uint8(4),
		uint8(1),
		uint16(30),
		uint16(10),
		uint16(60),
	)
	f.Add("", "", uint16(0), uint8(0), uint8(0), uint16(0), uint16(0), uint16(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		groupID string,
		maxPollRecords uint16,
		maxConcurrentFetches uint8,
		maxConcurrentHandlers uint8,
		handlerSeconds uint16,
		commitSeconds uint16,
		rebalanceSeconds uint16,
	) {
		config := validConsumerConfig()
		config.Topics = []string{topic}
		config.GroupID = groupID
		config.MaxPollRecords = int(maxPollRecords)
		config.MaxConcurrentFetches = int(maxConcurrentFetches)
		config.MaxConcurrentHandlers = int(maxConcurrentHandlers)
		config.HandlerTimeout = time.Duration(handlerSeconds) * time.Second
		config.CommitTimeout = time.Duration(commitSeconds) * time.Second
		config.RebalanceTimeout = time.Duration(rebalanceSeconds) * time.Second

		_, _ = normalizeConsumerConfig(config)
	})
}

func FuzzFailureHandlerConfig(f *testing.F) {
	f.Add(
		"events.retry.v1",
		uint8(FailureModeRetryTopic),
		uint8(3),
		uint16(1),
		uint16(10),
		uint8(ErrorRetryable),
		uint16(1),
	)
	f.Add("", uint8(255), uint8(0), uint16(0), uint16(0), uint8(0), uint16(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		mode uint8,
		attempts uint8,
		initialBackoffMilliseconds uint16,
		maxBackoffMilliseconds uint16,
		category uint8,
		version uint16,
	) {
		config := FailureHandlerConfig{
			Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
				return nil
			}),
			Mode: FailureMode(mode),
			Retry: FailureRetryPolicy{
				MaxAttempts: int(attempts),
				InitialBackoff: time.Duration(initialBackoffMilliseconds) *
					time.Millisecond,
				MaxBackoff: time.Duration(maxBackoffMilliseconds) *
					time.Millisecond,
				Categories: []ErrorCategory{ErrorCategory(category)},
			},
			Target: FailureTarget{Topic: topic, Version: version},
		}
		switch config.Mode {
		case FailureModeRetryTopic, FailureModeDeadLetter:
			config.Publisher = failurePublisherFunc(func(
				context.Context,
				ProducerRecord,
			) DeliveryResult {
				return DeliveryResult{}
			})
			config.PublishTimeout = time.Second
		case FailureModeDelegate:
			config.Target = FailureTarget{}
			config.Delegate = FailureDelegateFunc(func(
				context.Context,
				HandlerFailure,
			) error {
				return nil
			})
		default:
			config.Target = FailureTarget{}
		}

		_ = config.Validate()
	})
}

func FuzzMessageValidation(f *testing.F) {
	f.Add("events", uint16(8), uint16(16), uint8(2))
	f.Add("", uint16(0), uint16(0), uint8(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		keyBytes uint16,
		valueBytes uint16,
		headerCount uint8,
	) {
		limits := DefaultMessageLimits()
		message := Message{
			Topic: topic,
			Key:   []byte(strings.Repeat("k", int(keyBytes))),
			Value: []byte(strings.Repeat("v", int(valueBytes))),
		}
		for range int(headerCount % 65) {
			message.Headers = append(message.Headers, Header{
				Key:   "header",
				Value: []byte("value"),
			})
		}

		_ = message.validate(limits)
	})
}

func FuzzTransactionProcessorConfig(f *testing.F) {
	f.Add(
		"source-events",
		"derived-events",
		"transaction-worker-0",
		"2.5",
		uint16(100),
		uint16(1_000),
		uint32(10<<20),
		uint16(30),
		uint16(60),
	)
	f.Add(
		"",
		"",
		"",
		"2.4",
		uint16(0),
		uint16(0),
		uint32(0),
		uint16(0),
		uint16(0),
	)

	f.Fuzz(func(
		t *testing.T,
		sourceTopic string,
		outputTopic string,
		transactionalID string,
		minimumVersion string,
		maxPoll uint16,
		maxOutputs uint16,
		maxOutputBytes uint32,
		processingSeconds uint16,
		transactionSeconds uint16,
	) {
		config := validTransactionProcessorConfig()
		config.Connection.Protocol.MinimumVersion = minimumVersion
		config.Group.Topics = []string{sourceTopic}
		config.Group.MaxPollRecords = int(maxPoll)
		config.Group.ProcessingTimeout =
			time.Duration(processingSeconds) * time.Second
		config.Output.AllowedTopics = []string{outputTopic}
		config.Output.TransactionalID = transactionalID
		config.Output.MaxOutputRecords = int(maxOutputs)
		config.Output.MaxOutputBytes = int64(maxOutputBytes)
		config.Output.TransactionTimeout =
			time.Duration(transactionSeconds) * time.Second

		_, _ = normalizeTransactionProcessorConfig(config)
	})
}

func FuzzReplayConfig(f *testing.F) {
	f.Add("events", int32(0), int64(0), int64(1), uint16(100))
	f.Add("", int32(-1), int64(-1), int64(0), uint16(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		partition int32,
		start int64,
		end int64,
		maxPoll uint16,
	) {
		_, _ = normalizeReplayConfig(ReplayConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "fuzz-replay",
			Ranges: []ReplayRange{{
				Topic: topic, Partition: partition,
				StartOffset: start, EndOffset: end,
			}},
			MaxPollRecords: int(maxPoll),
		})
	})
}
