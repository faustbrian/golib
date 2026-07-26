package postgres

import "testing"

func BenchmarkEncodeMetadata(b *testing.B) {
	metadata := map[string]string{
		"source":      "benchmark",
		"environment": "test",
		"region":      "eu-north-1",
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = encodeMetadata(metadata)
	}
}
