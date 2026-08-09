package cloudevents

// Limits bounds untrusted event formats and protocol bindings before semantic
// parsing. Values are byte counts unless stated otherwise. Kafka-specific
// limits cover the record metadata copied by DecodeKafka, including metadata
// not owned by the CloudEvents binding.
type Limits struct {
	MaxEventBytes            int64
	MaxDataBytes             int64
	MaxAttributes            int
	MaxAttributeNameBytes    int
	MaxAttributeValueBytes   int
	MaxDepth                 int
	MaxBatchEvents           int
	MaxKafkaKeyBytes         int
	MaxKafkaHeaders          int
	MaxKafkaHeaderNameBytes  int
	MaxKafkaHeaderValueBytes int
}

// DefaultLimits accepts the CloudEvents interoperability floor while bounding
// allocations for ordinary library use.
func DefaultLimits() Limits {
	return Limits{
		MaxEventBytes:            1 << 20,
		MaxDataBytes:             1 << 20,
		MaxAttributes:            64,
		MaxAttributeNameBytes:    64,
		MaxAttributeValueBytes:   16 << 10,
		MaxDepth:                 64,
		MaxBatchEvents:           1000,
		MaxKafkaKeyBytes:         1 << 20,
		MaxKafkaHeaders:          64,
		MaxKafkaHeaderNameBytes:  256,
		MaxKafkaHeaderValueBytes: 16 << 10,
	}
}
