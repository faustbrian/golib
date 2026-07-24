package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
)

func TestMainUsesSignalLifecycleAndExitStatus(t *testing.T) {
	originalExit := processExit
	originalGetenv := processGetenv
	originalDependencies := processDependenciesFactory
	originalNotify := processNotify
	originalReport := processReport
	t.Cleanup(func() {
		processExit = originalExit
		processGetenv = originalGetenv
		processDependenciesFactory = originalDependencies
		processNotify = originalNotify
		processReport = originalReport
	})

	processGetenv = mapEnvironment(map[string]string{
		"DATABASE_URL":              "postgres://database/control",
		"QUEUE_CONTROL_ACCESS_FILE": "/run/secrets/access.json",
	})
	stopped := false
	processNotify = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		if parent == nil || len(signals) != 2 {
			t.Fatalf("notify input = (%v, %v)", parent, signals)
		}

		return parent, func() { stopped = true }
	}

	stageErr := errors.New("serve failed")
	for name, input := range map[string]struct {
		runErr error
		code   int
	}{
		"success": {},
		"failure": {runErr: stageErr, code: 1},
	} {
		t.Run(name, func(t *testing.T) {
			stopped = false
			dependencies := validProcessDependencies(t)
			dependencies.buildServer = processServerBuilder(input.runErr)
			processDependenciesFactory = func() processDependencies { return dependencies }

			exitCode := -1
			processExit = func(code int) { exitCode = code }
			var reported error
			processReport = func(err error) { reported = err }

			main()
			if exitCode != input.code || stopped != true {
				t.Fatalf("main() = exit %d, stopped %t, want %d and true", exitCode, stopped, input.code)
			}
			if !errors.Is(reported, input.runErr) {
				t.Fatalf("reported error = %v, want %v", reported, input.runErr)
			}
		})
	}

	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })
	reportProcessError(stageErr)
}

func processServerBuilder(runErr error) func(
	net.Listener,
	http.Handler,
	server.Config,
) (processServer, error) {
	return func(net.Listener, http.Handler, server.Config) (processServer, error) {
		return processServerFunc(func(context.Context) error { return runErr }), nil
	}
}
