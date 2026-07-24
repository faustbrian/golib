package valkey_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	invalidation "github.com/faustbrian/golib/pkg/authorization/valkey"
	native "github.com/valkey-io/valkey-go"
)

func TestIntegrationDurableInvalidationAndPolling(t *testing.T) {
	address := os.Getenv("VALKEY_ADDRESS")
	if address == "" {
		t.Skip("VALKEY_ADDRESS is not configured")
	}
	client, err := native.NewClient(native.ClientOption{
		InitAddress:       []string{address},
		PipelineMultiplex: -1,
	})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	defer client.Close()

	prefix := "authorization:integration"
	ctx := context.Background()
	if err := client.Do(ctx, client.B().Del().Key(prefix+":revision").Build()).Error(); err != nil {
		t.Fatalf("DEL revision error = %v", err)
	}
	invalidator, err := invalidation.New(client, invalidation.Options{
		Prefix: prefix, PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("invalidation.New() error = %v", err)
	}
	advanced, err := invalidator.Publish(ctx, 1)
	if err != nil || !advanced {
		t.Fatalf("Publish(1) = (%v, %v)", advanced, err)
	}
	advanced, err = invalidator.Publish(ctx, 1)
	if err != nil || advanced {
		t.Fatalf("duplicate Publish(1) = (%v, %v)", advanced, err)
	}
	revision, err := invalidator.Revision(ctx)
	if err != nil || revision != 1 {
		t.Fatalf("Revision() = (%d, %v)", revision, err)
	}

	admin, err := native.NewClient(native.ClientOption{InitAddress: []string{address}})
	if err != nil {
		t.Fatalf("valkey.NewClient(admin) error = %v", err)
	}
	defer admin.Close()
	clientID, err := client.Do(ctx, client.B().ClientId().Build()).ToInt64()
	if err != nil {
		t.Fatalf("CLIENT ID error = %v", err)
	}
	if err := admin.Do(ctx, admin.B().ClientKill().Id(clientID).Build()).Error(); err != nil {
		t.Fatalf("CLIENT KILL error = %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		revision, err = invalidator.Revision(ctx)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Revision() did not recover after connection termination: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if revision != 1 {
		t.Fatalf("reconnected Revision() = %d, want 1", revision)
	}

	watchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	observed := make(chan authorization.Revision, 1)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- invalidator.Watch(watchCtx, 1, func(revision authorization.Revision) error {
			observed <- revision
			return errors.New("observed")
		})
	}()
	if _, err := invalidator.Publish(ctx, 2); err != nil {
		t.Fatalf("Publish(2) error = %v", err)
	}
	select {
	case revision := <-observed:
		if revision != 2 {
			t.Errorf("observed revision = %d, want 2", revision)
		}
	case <-watchCtx.Done():
		t.Fatal("watcher did not observe revision 2")
	}
	if err := <-watchDone; err == nil {
		t.Fatal("Watch() returned nil, want handler error")
	}
}
