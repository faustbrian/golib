package job

import (
	"testing"
	"time"
)

func FuzzDecodeE(f *testing.F) {
	message := NewTask(nil)
	f.Add(Encode(&message), DefaultMaxMessageBytes)
	f.Add([]byte("not-json"), DefaultMaxMessageBytes)
	f.Add([]byte("{}"), 1)

	f.Fuzz(func(t *testing.T, data []byte, maxBytes int) {
		decoded, err := DecodeE(data, maxBytes)
		if err == nil && decoded == nil {
			t.Fatal("successful decode returned a nil message")
		}
	})
}

func FuzzMessageValidation(f *testing.F) {
	f.Add(int64(0), int64(0), float64(2), int64(time.Second), int64(time.Minute))
	f.Add(int64(-1), int64(-1), float64(-1), int64(-1), int64(-1))

	f.Fuzz(func(
		t *testing.T,
		retryCount int64,
		retryDelay int64,
		retryFactor float64,
		retryMin int64,
		retryMax int64,
	) {
		message := Message{
			Timeout:     time.Minute,
			RetryCount:  retryCount,
			RetryDelay:  time.Duration(retryDelay),
			RetryFactor: retryFactor,
			RetryMin:    time.Duration(retryMin),
			RetryMax:    time.Duration(retryMax),
		}
		_ = message.Validate()
	})
}
