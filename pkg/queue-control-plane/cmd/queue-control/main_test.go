package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/cli"
)

func TestRunBuildsAuthenticatedClientFromEnvironment(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Queue-Control-Key-ID") != "operator-1" ||
			request.Header.Get("X-Queue-Control-Key") != "secret-123" {
			t.Errorf("API key headers = %v", request.Header)
		}
		_ = json.NewEncoder(writer).Encode(apihttp.WorkerPage{})
	}))
	defer server.Close()
	getenv := func(key string) string {
		return map[string]string{
			"QUEUE_CONTROL_URL":    server.URL,
			"QUEUE_CONTROL_KEY_ID": "operator-1",
			"QUEUE_CONTROL_KEY":    "secret-123",
		}[key]
	}
	var output bytes.Buffer
	exit := run(context.Background(), []string{"workers", "list", "--tenant", "tenant-1"}, getenv, &output, &bytes.Buffer{}, server.Client())
	if exit != cli.ExitOK || output.String() == "" {
		t.Fatalf("run() = %d, output %q", exit, output.String())
	}
}

func TestRunRejectsMissingOrInvalidEnvironment(t *testing.T) {
	t.Parallel()

	tests := map[string]func(string) string{
		"missing": func(string) string { return "" },
		"invalid URL": func(key string) string {
			if key == "QUEUE_CONTROL_URL" {
				return "://invalid"
			}
			return "credential"
		},
	}
	for name, getenv := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			if exit := run(context.Background(), nil, getenv, &bytes.Buffer{}, &stderr, http.DefaultClient); exit != cli.ExitFailure {
				t.Fatalf("run() = %d, want %d", exit, cli.ExitFailure)
			}
			if stderr.String() == "" {
				t.Fatal("stderr is empty")
			}
		})
	}
}

func TestEnvironmentAPIKeySourceReturnsConfiguredCredentials(t *testing.T) {
	t.Parallel()

	id, secret, err := (environmentAPIKeySource{id: "operator-1", secret: "secret-123"}).APIKey(context.Background())
	if err != nil || id != "operator-1" || secret != "secret-123" {
		t.Fatalf("APIKey() = (%q, %q, %v)", id, secret, err)
	}
}

func TestMainExitsWithRunResult(t *testing.T) {
	t.Setenv("QUEUE_CONTROL_URL", "")
	previous := processExit
	defer func() { processExit = previous }()
	processExit = func(code int) { panic(code) }
	defer func() {
		if recovered := recover(); recovered != cli.ExitFailure {
			t.Fatalf("main() exit = %v, want %d", recovered, cli.ExitFailure)
		}
	}()

	main()
}
