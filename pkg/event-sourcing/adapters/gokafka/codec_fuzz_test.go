package gokafka

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
)

func FuzzRecordCodecDecode(f *testing.F) {
	codec := testRecordCodec(f)
	seed := testEncodedRecord(f, codec)
	f.Add(
		seed.Topic,
		seed.Key,
		seed.Value,
		"traceparent",
		[]byte("00-opaque"),
	)
	f.Add("denied", []byte{}, []byte{}, "es.future", []byte{0xff})

	f.Fuzz(func(
		t *testing.T,
		topic string,
		key []byte,
		value []byte,
		headerKey string,
		headerValue []byte,
	) {
		record := consumedRecord(seed)
		record.Topic = topic
		record.Key = key
		record.Value = value
		record.Headers = append(record.Headers, kafka.Header{
			Key:   headerKey,
			Value: headerValue,
		})

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
	})
}
