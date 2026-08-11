package eventoutbox

import (
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
)

func BenchmarkEnvelopeCodecRoundTrip(b *testing.B) {
	codec := mustCodec(b, FixedTopic("events"), outbox.DefaultLimits())
	message := internalMessage(b, true)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		envelope, err := codec.Encode(message)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := codec.Decode(envelope); err != nil {
			b.Fatal(err)
		}
	}
}
