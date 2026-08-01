package awssecretsmanager_test

import (
	"context"
	"fmt"

	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awsconfig "github.com/faustbrian/golib/pkg/config/adapters/awssecretsmanager"
)

type exampleClient struct{}

func (exampleClient) GetSecretValue(
	context.Context,
	*secretsmanager.GetSecretValueInput,
	...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
	payload := `{"database":{"url":"postgres://synthetic"}}`

	return &secretsmanager.GetSecretValueOutput{SecretString: &payload}, nil
}

func ExampleNew() {
	source, err := awsconfig.New(exampleClient{}, awsconfig.Options{
		Name:     "runtime-secrets",
		SecretID: "track/production/runtime",
	})
	if err != nil {
		panic(err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println(document.Tree["database"] != nil)
	// Output: true
}
