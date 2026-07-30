package awssecretsmanager

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

func BenchmarkPutVersionCreate(b *testing.B) {
	request := validPutVersionRequest()
	client := &stubClient{
		createOutput: &secretsmanager.CreateSecretOutput{
			ARN: aws.String(
				"arn:aws:secretsmanager:eu-north-1:000000000000:secret:synthetic",
			),
			VersionId: aws.String(request.VersionID),
		},
	}
	store, err := New(client, "alias/track-secrets")
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.PutVersion(
			context.Background(),
			request,
		); err != nil {
			b.Fatalf("PutVersion() error = %v", err)
		}
	}
}
