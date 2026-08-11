package gokafka

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

func FuzzRecordCodecDecode(f *testing.F) {
	codec := testRecordCodec(f)
	seed := testEncodedRecord(f, codec)
	f.Add(
		seed.Topic,
		seed.Key,
		seed.Value,
		seed.Timestamp.UnixNano(),
		int8(kafka.TimestampCreateTime),
		encodeFuzzHeaders(seed.Headers),
	)
	f.Add(
		"töpïc",
		[]byte{},
		[]byte{},
		int64(0),
		int8(kafka.TimestampLogAppendTime),
		encodeFuzzHeaders([]kafka.Header{
			{Key: HeaderApplicationMetadata, Value: []byte(`{"dup":"a","dup":"b"}`)},
			{Key: HeaderStreamVersion, Value: []byte("01")},
			{Key: HeaderRecordedAt, Value: []byte("2026-07-25T10:11:12.000000Z")},
			{Key: HeaderMessageID, Value: []byte("duplicate")},
			{Key: HeaderMessageID, Value: []byte("duplicate")},
		}),
	)

	f.Fuzz(func(
		t *testing.T,
		topic string,
		key []byte,
		value []byte,
		timestampUnixNano int64,
		timestampType int8,
		headerBytes []byte,
	) {
		record := kafka.ConsumedMessage{
			Topic:         topic,
			Key:           slices.Clone(key),
			Value:         slices.Clone(value),
			Headers:       decodeFuzzHeaders(headerBytes),
			Timestamp:     time.Unix(0, timestampUnixNano).UTC(),
			TimestampType: kafka.TimestampType(timestampType),
		}

		delivery, err := codec.Decode(record)
		if err != nil {
			if !errors.Is(err, ErrRecordCorrupt) {
				t.Fatalf("unexpected error category: %v", err)
			}

			return
		}
		if delivery.IsZero() {
			t.Fatal("successful decode returned a zero delivery")
		}
		canonical, err := codec.Encode(delivery)
		if err != nil {
			t.Fatalf("re-encode successful decode: %v", err)
		}
		roundTrip, err := codec.Decode(consumedRecord(canonical))
		if err != nil {
			t.Fatalf("decode canonical re-encoding: %v", err)
		}
		if roundTrip.Mode() != delivery.Mode() ||
			!roundTrip.Message().Equal(delivery.Message()) {
			t.Fatal("canonical re-encoding changed delivery")
		}
	})
}

func encodeFuzzHeaders(headers []kafka.Header) []byte {
	encoded := make([]byte, 0)
	for _, header := range headers {
		keyBytes := []byte(header.Key)
		if len(keyBytes) > int(^uint16(0)) ||
			len(header.Value) > int(^uint16(0)) {
			continue
		}
		encoded = binary.LittleEndian.AppendUint16(encoded, uint16(len(keyBytes)))
		encoded = binary.LittleEndian.AppendUint16(encoded, uint16(len(header.Value)))
		encoded = append(encoded, keyBytes...)
		encoded = append(encoded, header.Value...)
	}

	return encoded
}

func decodeFuzzHeaders(encoded []byte) []kafka.Header {
	headers := make([]kafka.Header, 0)
	for len(encoded) >= 4 && len(headers) <= DefaultRecordLimits().MaxHeaders {
		keyLength := int(binary.LittleEndian.Uint16(encoded[:2]))
		valueLength := int(binary.LittleEndian.Uint16(encoded[2:4]))
		encoded = encoded[4:]
		if keyLength > len(encoded) || valueLength > len(encoded)-keyLength {
			break
		}
		headers = append(headers, kafka.Header{
			Key:   string(encoded[:keyLength]),
			Value: slices.Clone(encoded[keyLength : keyLength+valueLength]),
		})
		encoded = encoded[keyLength+valueLength:]
	}

	return headers
}
