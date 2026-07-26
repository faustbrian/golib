package httpclient

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestLifecycleLeakCloseCancelsPendingOperation(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan error, 1)
	client, err := New(Config{Transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	go func() {
		_, doErr := client.Do(request)
		finished <- doErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("pending operation did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	select {
	case doErr := <-finished:
		if !errors.Is(doErr, context.Canceled) {
			t.Fatalf("pending operation error = %v", doErr)
		}
	case <-time.After(time.Second):
		t.Fatal("pending operation goroutine leaked after close")
	}
}
