package mskiam_test

import (
	"context"
	"testing"
	"time"

	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

func BenchmarkToken(b *testing.B) {
	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: staticCredentialsProvider{},
		TokenTimeout:        time.Second,
	})
	if err != nil {
		b.Fatalf("new provider: %v", err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, tokenErr := provider.Token(ctx); tokenErr != nil {
			b.Fatalf("generate token: %v", tokenErr)
		}
	}
}
