package awssecretsmanager

import (
	"context"
	"testing"

	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

func BenchmarkSourceLoad(b *testing.B) {
	payload := `{"database":{"url":"postgres://synthetic"},"workers":4}`
	source, err := New(&stubClient{
		output: &secretsmanager.GetSecretValueOutput{SecretString: &payload},
	}, Options{Name: "benchmark", SecretID: "track/benchmark"})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := source.Load(context.Background()); err != nil {
			b.Fatalf("Load() error = %v", err)
		}
	}
}
