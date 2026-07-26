package kafka

import "time"

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
