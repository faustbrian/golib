package goqueue

import "testing"

func BenchmarkCodecEncode(b *testing.B) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	delivery := minimalQueueDelivery(b)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Encode(delivery); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodecDecode(b *testing.B) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	delivery := minimalQueueDelivery(b)
	encoded, err := codec.Encode(delivery)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Decode(encoded); err != nil {
			b.Fatal(err)
		}
	}
}
