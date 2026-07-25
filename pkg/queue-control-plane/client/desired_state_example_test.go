package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/client"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func ExampleClient_DesiredStateReader() {
	target := queue.Target{Kind: queue.TargetQueue, Name: "critical"}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(queue.DesiredRecord{
			Target: target, State: queue.DesiredPaused, Revision: 3,
			ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
			CommandID: "pause-critical-3",
		})
	}))
	defer server.Close()

	api, err := client.New(client.Config{
		BaseURL: server.URL,
		APIKeys: exampleAPIKeySource{},
	})
	if err != nil {
		panic(err)
	}
	reader, err := api.DesiredStateReader("tenant-1")
	if err != nil {
		panic(err)
	}
	reconciler, err := queue.NewDesiredStateReconciler(
		queue.DesiredStateReconcilerConfig{
			Reader: reader, Applier: exampleDesiredApplier{},
			Targets: []queue.Target{target},
		},
	)
	if err != nil {
		panic(err)
	}
	if err := reconciler.Reconcile(context.Background()); err != nil {
		panic(err)
	}
	// Output: apply paused to critical at revision 3
}

type exampleAPIKeySource struct{}

func (exampleAPIKeySource) APIKey(context.Context) (string, string, error) {
	return "worker-1", "example-secret", nil
}

type exampleDesiredApplier struct{}

func (exampleDesiredApplier) ApplyDesiredState(
	_ context.Context,
	record queue.DesiredRecord,
) error {
	fmt.Printf(
		"apply %s to %s at revision %d\n",
		record.State,
		record.Target.Name,
		record.Revision,
	)
	return nil
}
