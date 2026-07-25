package cli_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestShutdownControllerSupportsGracefulThenForcedPolicy(t *testing.T) {
	t.Parallel()

	controller, err := cli.NewShutdownController(context.Background())
	if err != nil {
		t.Fatalf("create shutdown controller: %v", err)
	}
	cause := errors.New("SIGTERM")
	if action := controller.Signal(cause); action != cli.ShutdownGraceful {
		t.Fatalf("first signal action = %v, want graceful", action)
	}
	if !errors.Is(context.Cause(controller.Context()), cause) {
		t.Fatalf("context cause = %v, want %v", context.Cause(controller.Context()), cause)
	}
	select {
	case <-controller.Forced():
		t.Fatal("forced channel closed after first signal")
	default:
	}

	if action := controller.Signal(errors.New("SIGTERM repeated")); action != cli.ShutdownForced {
		t.Fatalf("second signal action = %v, want forced", action)
	}
	select {
	case <-controller.Forced():
	default:
		t.Fatal("forced channel remains open after second signal")
	}
	if action := controller.Signal(errors.New("third")); action != cli.ShutdownAlreadyForced {
		t.Fatalf("third signal action = %v, want already forced", action)
	}
}

func TestShutdownControllerIsConcurrentAndNilSafe(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if _, err := cli.NewShutdownController(nilContext); err == nil {
		t.Fatal("NewShutdownController(nil) error = nil")
	}
	controller, err := cli.NewShutdownController(context.Background())
	if err != nil {
		t.Fatalf("create shutdown controller: %v", err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			controller.Signal(nil)
		}()
	}
	wait.Wait()
	select {
	case <-controller.Forced():
	default:
		t.Fatal("concurrent repeated signals did not force shutdown")
	}
}

func TestShutdownControllerCloseReleasesGracefulContext(t *testing.T) {
	t.Parallel()

	controller, err := cli.NewShutdownController(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	controller.Close()
	controller.Close()
	if !errors.Is(context.Cause(controller.Context()), context.Canceled) {
		t.Fatalf("close cause = %v, want context cancellation", context.Cause(controller.Context()))
	}
	if action := controller.Signal(errors.New("late signal")); action != cli.ShutdownAlreadyForced {
		t.Fatalf("signal after close = %v, want already forced", action)
	}
	select {
	case <-controller.Forced():
		t.Fatal("Close reported a forced signal")
	default:
	}

	var nilController *cli.ShutdownController
	nilController.Close()
}
