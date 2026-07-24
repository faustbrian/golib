package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var (
	processExit                = os.Exit
	processGetenv              = os.Getenv
	processDependenciesFactory = productionDependencies
	processNotify              = signal.NotifyContext
	processReport              = reportProcessError
)

func main() {
	processExit(executeMain())
}

func executeMain() int {
	ctx, stop := processNotify(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := runProcess(ctx, processGetenv, processDependenciesFactory())
	if err != nil {
		processReport(err)

		return 1
	}

	return 0
}

func reportProcessError(err error) {
	slog.Error("queue control plane stopped", "error", err)
}
