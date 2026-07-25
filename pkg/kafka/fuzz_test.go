package kafka

import (
	"strings"
	"testing"
)

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
