package goqueue

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
)

func FuzzCodecDecode(f *testing.F) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		f.Fatalf("NewCodec() error = %v", err)
	}
	f.Add([]byte(`{"format":"golib.event-sourcing.queue.v1"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > job.DefaultMaxMessageBytes+1 {
			t.Skip()
		}
		_, err := codec.Decode(input)
		if err != nil &&
			!errors.Is(err, ErrEnvelopeInvalid) &&
			!errors.Is(err, ErrEnvelopeTooLarge) {
			t.Fatalf("Decode() error = %v", err)
		}
	})
}
