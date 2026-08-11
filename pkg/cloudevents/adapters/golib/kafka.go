package golib

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	"github.com/faustbrian/golib/pkg/kafka"
)

// KafkaTransport is Kafka-owned producer metadata retained outside the
// CloudEvents binding. Headers must not use CloudEvents-owned names.
type KafkaTransport struct {
	Topic     string
	Partition kafka.PartitionSelection
	Key       []byte
	Headers   []kafka.Header
	Timestamp time.Time
}

// KafkaState is Kafka-owned consumed-record state. It is never promoted to
// CloudEvents context attributes.
type KafkaState struct {
	Topic         string
	Timestamp     time.Time
	TimestampType kafka.TimestampType
	Partition     int32
	Offset        int64
	LeaderEpoch   int32
}

// EncodeKafka applies the official CloudEvents Kafka binding and then attaches
// caller-owned Kafka transport metadata without hidden broker I/O.
func EncodeKafka(
	event cloudevents.Event,
	mode cloudevents.ContentMode,
	transport KafkaTransport,
) (kafka.ProducerRecord, error) {
	key := cloneBytes(transport.Key)
	if partitionKey, present := cloudevents.KafkaPartitionKey(event); present {
		if key != nil && !bytes.Equal(key, partitionKey) {
			return kafka.ProducerRecord{}, fmt.Errorf("%w: kafka key", ErrMetadataCollision)
		}
		key = partitionKey
	}
	for _, header := range transport.Headers {
		if header.Key == "content-type" || strings.HasPrefix(header.Key, "ce_") {
			return kafka.ProducerRecord{}, fmt.Errorf("%w: kafka header %s", ErrMetadataCollision, header.Key)
		}
	}
	binding, err := cloudevents.EncodeKafka(event, mode, key)
	if err != nil {
		return kafka.ProducerRecord{}, err
	}
	var headers []kafka.Header
	for _, header := range binding.Headers {
		headers = append(headers, kafka.Header{Key: header.Key, Value: cloneBytes(header.Value)})
	}
	for _, header := range transport.Headers {
		headers = append(headers, kafka.Header{Key: header.Key, Value: cloneBytes(header.Value)})
	}
	return kafka.ProducerRecord{
		Topic: transport.Topic, Partition: transport.Partition,
		Key: cloneBytes(binding.Key), Value: cloneBytes(binding.Value),
		Headers: headers, Timestamp: transport.Timestamp,
	}, nil
}

// DecodeKafka applies the official binding to one borrowed Golib Kafka record
// and returns transport-owned state separately. Returned bytes do not alias the
// consumed record.
func DecodeKafka(
	record kafka.ConsumedRecord,
	limits cloudevents.Limits,
) (cloudevents.KafkaMessage, KafkaState, error) {
	if len(record.Headers) > limits.MaxKafkaHeaders {
		return cloudevents.KafkaMessage{}, KafkaState{}, cloudevents.ErrLimitExceeded
	}
	headers := make([]cloudevents.KafkaHeader, len(record.Headers))
	for index, header := range record.Headers {
		headers[index] = cloudevents.KafkaHeader{Key: header.Key, Value: header.Value}
	}
	message, err := cloudevents.DecodeKafka(cloudevents.KafkaRecord{
		Key: record.Key, Value: record.Value, Headers: headers,
	}, limits)
	if err != nil {
		return cloudevents.KafkaMessage{}, KafkaState{}, err
	}
	return message, KafkaState{
		Topic: record.Topic, Timestamp: record.Timestamp, TimestampType: record.TimestampType,
		Partition: record.Partition, Offset: record.Offset, LeaderEpoch: record.LeaderEpoch,
	}, nil
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}
