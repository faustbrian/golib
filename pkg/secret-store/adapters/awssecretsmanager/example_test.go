package awssecretsmanager_test

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awssecretstore "github.com/faustbrian/golib/pkg/secret-store/adapters/awssecretsmanager"
)

func ExampleStore_PutVersion() {
	ctx := context.Background()
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		panic(err)
	}
	store, err := awssecretstore.New(
		secretsmanager.NewFromConfig(awsConfig),
		"alias/track-secrets",
	)
	if err != nil {
		panic(err)
	}
	reference, err := store.PutVersion(
		ctx,
		awssecretstore.PutVersionRequest{
			Name: "track/carrier-account/01K4ZVHQ8R7XH2WTB9F2T9G6HY",
			VersionID: "6e45f31f4601a3af2e99f2f479d69165" +
				"0fb71cd2d93a76872f4e9f5fa7b42135",
			Stage: "track-migration-6e45f31f4601a3af2e99f2f479d69165" +
				"0fb71cd2d93a76872f4e9f5fa7b42135",
			Value: []byte(`{"client_id":"synthetic"}`),
		},
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(reference.VersionID)
}
