// Command process-fixture proves the narrow executable integration boundary.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func main() {
	awaitSignal := cli.BoolOption("await-signal")
	application, err := cli.Compile(cli.NewCommand(
		"process-fixture",
		cli.WithOptions(awaitSignal),
		cli.WithInteraction(cli.InteractionForbidden),
		cli.WithHandler(func(ctx context.Context, invocation cli.Invocation) error {
			if awaitSignal.Get(invocation.Input()) {
				<-ctx.Done()
				return ctx.Err()
			}
			return invocation.Output().SetData(struct {
				Status string `json:"status"`
			}{Status: "ok"})
		}),
	))
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fixture construction failed")
		os.Exit(70)
	}
	controller, err := cli.NewShutdownController(context.Background())
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fixture shutdown setup failed")
		os.Exit(70)
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case delivered := <-signals:
				if controller.Signal(errors.New(delivered.String())) == cli.ShutdownForced {
					os.Exit(137)
				}
			case <-done:
				return
			}
		}
	}()
	for _, argument := range os.Args[1:] {
		if argument == "--await-signal" {
			_, _ = fmt.Fprintln(os.Stderr, "ready")
			break
		}
	}

	result := application.Run(controller.Context(), cli.Request{
		Args: os.Args[1:], Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		NonInteractive: true,
		Output:         cli.OutputPolicy{Mode: cli.OutputJSON, NoColor: true},
	})
	signal.Stop(signals)
	close(done)
	wait.Wait()
	controller.Close()
	os.Exit(result.ExitCode)
}
