package httpclient

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(main *testing.M) {
	http.DefaultTransport = TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected use of process-global HTTP transport")
	})
	goleak.VerifyTestMain(main)
}

func receiveTestValue[Value any](t *testing.T, values <-chan Value) Value {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for test coordination")
		var zero Value
		return zero
	}
}

func closeTestSignal(signal chan struct{}) {
	select {
	case <-signal:
	default:
		close(signal)
	}
}
