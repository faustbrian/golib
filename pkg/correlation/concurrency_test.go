package correlation_test

import (
	"context"
	"sync"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

func TestDefaultFactoryAndContextAreConcurrentAndUnique(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 128
	identifiers := make(chan string, workers*2)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			values, err := factory.Start()
			if err != nil {
				t.Error(err)
				return
			}
			ctx := correlation.WithValues(context.Background(), values)
			stored, ok := correlation.FromContext(ctx)
			if !ok || stored != values {
				t.Errorf("stored values = %#v, %v", stored, ok)
				return
			}
			identifiers <- values.CorrelationID.String()
			identifiers <- values.RequestID.String()
		})
	}
	group.Wait()
	close(identifiers)
	seen := make(map[string]struct{}, workers*2)
	for identifier := range identifiers {
		if _, exists := seen[identifier]; exists {
			t.Fatalf("duplicate generated identifier %q", identifier)
		}
		seen[identifier] = struct{}{}
	}
	if len(seen) != workers*2 {
		t.Fatalf("generated %d unique identifiers, want %d", len(seen), workers*2)
	}
}

func TestDeterministicCodecAndContextAreConcurrentAndAliasSafe(t *testing.T) {
	key := []byte("private-key")
	strategy, err := correlation.NewDeterministic(correlation.DeterministicOptions{
		Domain: "workflow", Version: 1, Key: key, Length: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := strategy.Derive([]byte("business-input"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range key {
		key[index] = 'x'
	}
	codec, _ := correlation.NewCodec(correlation.CodecOptions{})
	carrier := memoryCarrier{
		correlation.DefaultCorrelationField: {want.String()},
		correlation.DefaultRequestField:     {"request"},
	}

	const workers = 128
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			derived, err := strategy.Derive([]byte("business-input"))
			if err != nil || derived != want {
				t.Errorf("Derive() = %q, %v; want %q", derived, err, want)
				return
			}
			values, err := codec.Extract(carrier)
			if err != nil {
				t.Error(err)
				return
			}
			ctx := correlation.WithValues(context.Background(), values)
			stored, ok := correlation.FromContext(ctx)
			if !ok || stored != values {
				t.Errorf("stored values = %#v, %v", stored, ok)
			}
		})
	}
	group.Wait()
}
