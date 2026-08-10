package glue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestProviderConcurrencyBudgetCancelsQueuedRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	definition := `"string"`
	version := int64(1)
	provider := internalProvider(t, &apiFunction{
		version: func(context.Context, *awsglue.GetSchemaVersionInput) (*awsglue.GetSchemaVersionOutput, error) {
			calls.Add(1)
			close(started)
			<-release
			return &awsglue.GetSchemaVersionOutput{
				SchemaVersionId: pointer(internalVersionID), SchemaDefinition: &definition,
				DataFormat: types.DataFormatAvro, Status: types.SchemaVersionStatusAvailable,
				VersionNumber: &version,
			}, nil
		},
	})
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{
		Provider: ProviderName, Scope: "scope", Value: internalVersionID,
	})

	first := make(chan error, 1)
	go func() {
		_, err := provider.Resolve(context.Background(), lookup)
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not acquire provider budget")
	}

	waiterContext, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		_, err := provider.Resolve(waiterContext, lookup)
		second <- err
	}()
	cancel()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued request error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued request ignored cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("API calls while budget occupied = %d", calls.Load())
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first request error = %v", err)
	}
}
