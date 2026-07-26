package kafka

import "time"

// TopicPartition identifies one exact Kafka partition. It does not include a
// consumer generation or imply current assignment ownership.
type TopicPartition struct {
	Topic     string
	Partition int32
}

// PartitionSelectionMode identifies automatic or explicit producer partition
// selection. The zero value preserves Kafka key-based or unkeyed partitioning.
type PartitionSelectionMode uint8

const (
	// PartitionAutomatic delegates partition selection to the producer's
	// automatic keyed or unkeyed partitioner.
	PartitionAutomatic PartitionSelectionMode = iota
	// PartitionExplicit sends the record to one exact Kafka partition.
	PartitionExplicit
)

// PartitionSelection is one immutable-by-value producer partition decision.
// Automatic selection requires Partition to remain zero. Explicit selections
// require a non-negative Partition and are validated before admission.
type PartitionSelection struct {
	Mode      PartitionSelectionMode
	Partition int32
}

// ExplicitPartition selects one exact non-negative Kafka partition. A negative
// value is retained so normal record validation can return a classifiable
// error without panicking during record construction.
func ExplicitPartition(partition int32) PartitionSelection {
	return PartitionSelection{Mode: PartitionExplicit, Partition: partition}
}

// TimestampType identifies how Kafka assigned a record timestamp.
type TimestampType int8

const (
	// TimestampUnknown identifies records from message formats without a
	// timestamp.
	TimestampUnknown TimestampType = -1
	// TimestampCreateTime identifies a timestamp assigned by the producer.
	TimestampCreateTime TimestampType = 0
	// TimestampLogAppendTime identifies a timestamp assigned by the broker.
	TimestampLogAppendTime TimestampType = 1
)

// ProducerRecord is one Kafka record submitted for production. The producer
// copies all byte slices before retaining or passing the record to franz-go.
type ProducerRecord struct {
	Topic     string
	Partition PartitionSelection
	Key       []byte
	Value     []byte
	Headers   []Header
	Timestamp time.Time
}

// Message is retained as the pre-v1 name for ProducerRecord.
type Message = ProducerRecord

// ConsumedRecord is one borrowed Kafka record. Key, value, header values, and
// the header slice remain valid only for the synchronous handler call unless
// Retain is used.
type ConsumedRecord struct {
	Topic         string
	Key           []byte
	Value         []byte
	Headers       []Header
	Timestamp     time.Time
	TimestampType TimestampType
	Partition     int32
	Offset        int64
	LeaderEpoch   int32
}

// ConsumedMessage is retained as the pre-v1 name for ConsumedRecord.
type ConsumedMessage = ConsumedRecord

// Retain returns a deep copy whose bytes remain owned by the caller.
func (record ConsumedRecord) Retain() ConsumedRecord {
	retained := record
	retained.Key = cloneBytes(record.Key)
	retained.Value = cloneBytes(record.Value)
	retained.Headers = cloneHeaders(record.Headers)

	return retained
}

func (record ProducerRecord) owned() ProducerRecord {
	owned := record
	owned.Key = cloneBytes(record.Key)
	owned.Value = cloneBytes(record.Value)
	owned.Headers = cloneHeaders(record.Headers)

	return owned
}

func validKafkaTopicName(name string, maximumBytes int) bool {
	if name == "" || name == "." || name == ".." || len(name) > maximumBytes {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}

		return false
	}

	return true
}

func cloneHeaders(headers []Header) []Header {
	if headers == nil {
		return nil
	}

	cloned := make([]Header, len(headers))
	for index, header := range headers {
		cloned[index] = Header{Key: header.Key, Value: cloneBytes(header.Value)}
	}

	return cloned
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}

	return append([]byte(nil), value...)
}
