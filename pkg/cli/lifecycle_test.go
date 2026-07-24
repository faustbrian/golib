package cli_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestLifecycleRunsInDeterministicOrder(t *testing.T) {
	t.Parallel()

	var events []string
	record := func(event string) { events = append(events, event) }
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithValidation(func(_ context.Context, _ cli.Input) error {
			record("validate")
			return nil
		}),
		cli.WithMiddleware(func(ctx context.Context, metadata cli.CommandMetadata, next cli.Next) error {
			record("middleware-before:" + metadata.Name())
			err := next(context.WithValue(ctx, lifecycleContextKey{}, "middleware"))
			record("middleware-after")
			return err
		}),
		cli.WithPreRun(func(ctx context.Context, _ cli.Invocation) error {
			record("pre:" + ctx.Value(lifecycleContextKey{}).(string))
			return nil
		}),
		cli.WithHandler(func(_ context.Context, _ cli.Invocation) error {
			record("run")
			return nil
		}),
		cli.WithPostRun(func(_ context.Context, _ cli.Invocation) error {
			record("post")
			return nil
		}),
		cli.WithCleanup(func(_ context.Context, _ cli.Invocation) error {
			record("cleanup-second")
			return nil
		}),
		cli.WithCleanup(func(_ context.Context, _ cli.Invocation) error {
			record("cleanup-first")
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{})
	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
	want := []string{
		"validate",
		"middleware-before:tool",
		"pre:middleware",
		"run",
		"post",
		"middleware-after",
		"cleanup-first",
		"cleanup-second",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestLifecycleFailureSemantics(t *testing.T) {
	t.Parallel()

	validationFailure := errors.New("validation failure")
	middlewareFailure := errors.New("middleware failure")
	preFailure := errors.New("pre failure")
	runFailure := errors.New("run failure")
	postFailure := errors.New("post failure")
	cleanupFailure := errors.New("cleanup failure")

	tests := []struct {
		name       string
		fail       string
		wantEvents []string
		wantCause  error
		wantKind   cli.ErrorKind
	}{
		{
			name:       "validation stops before middleware and cleanup",
			fail:       "validation",
			wantEvents: []string{"validation"},
			wantCause:  validationFailure,
			wantKind:   cli.ErrorKindValidation,
		},
		{
			name:       "middleware short circuit still cleans up",
			fail:       "middleware",
			wantEvents: []string{"validation", "middleware", "cleanup"},
			wantCause:  middlewareFailure,
			wantKind:   cli.ErrorKindCommand,
		},
		{
			name:       "pre failure skips run and post",
			fail:       "pre",
			wantEvents: []string{"validation", "middleware", "pre", "cleanup"},
			wantCause:  preFailure,
			wantKind:   cli.ErrorKindCommand,
		},
		{
			name:       "run failure skips post",
			fail:       "run",
			wantEvents: []string{"validation", "middleware", "pre", "run", "cleanup"},
			wantCause:  runFailure,
			wantKind:   cli.ErrorKindCommand,
		},
		{
			name:       "post failure preserves post error",
			fail:       "post",
			wantEvents: []string{"validation", "middleware", "pre", "run", "post", "cleanup"},
			wantCause:  postFailure,
			wantKind:   cli.ErrorKindCommand,
		},
		{
			name:       "cleanup failure is classified",
			fail:       "cleanup",
			wantEvents: []string{"validation", "middleware", "pre", "run", "post", "cleanup"},
			wantCause:  cleanupFailure,
			wantKind:   cli.ErrorKindCleanup,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var events []string
			command := cli.NewCommand(
				"tool",
				cli.WithValidation(func(_ context.Context, _ cli.Input) error {
					events = append(events, "validation")
					if test.fail == "validation" {
						return validationFailure
					}
					return nil
				}),
				cli.WithMiddleware(func(ctx context.Context, _ cli.CommandMetadata, next cli.Next) error {
					events = append(events, "middleware")
					if test.fail == "middleware" {
						return middlewareFailure
					}
					return next(ctx)
				}),
				cli.WithPreRun(func(_ context.Context, _ cli.Invocation) error {
					events = append(events, "pre")
					if test.fail == "pre" {
						return preFailure
					}
					return nil
				}),
				cli.WithHandler(func(_ context.Context, _ cli.Invocation) error {
					events = append(events, "run")
					if test.fail == "run" {
						return runFailure
					}
					return nil
				}),
				cli.WithPostRun(func(_ context.Context, _ cli.Invocation) error {
					events = append(events, "post")
					if test.fail == "post" {
						return postFailure
					}
					return nil
				}),
				cli.WithCleanup(func(_ context.Context, _ cli.Invocation) error {
					events = append(events, "cleanup")
					if test.fail == "cleanup" {
						return cleanupFailure
					}
					return nil
				}),
			)
			application, err := cli.Compile(command)
			if err != nil {
				t.Fatalf("compile command: %v", err)
			}

			result := application.Run(context.Background(), cli.Request{})
			if !errors.Is(result.Err, test.wantCause) {
				t.Fatalf("Run() error = %v, want cause %v", result.Err, test.wantCause)
			}
			var classified *cli.Error
			if !errors.As(result.Err, &classified) || classified.Kind() != test.wantKind {
				t.Fatalf("Run() error = %v, want kind %s", result.Err, test.wantKind)
			}
			if !reflect.DeepEqual(events, test.wantEvents) {
				t.Fatalf("events = %v, want %v", events, test.wantEvents)
			}
		})
	}
}

func TestCleanupFailureDoesNotErasePrimaryFailure(t *testing.T) {
	t.Parallel()

	primary := errors.New("primary")
	cleanup := errors.New("cleanup")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(context.Context, cli.Invocation) error { return primary }),
		cli.WithCleanup(func(context.Context, cli.Invocation) error { return cleanup }),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{})
	if !errors.Is(result.Err, primary) || !errors.Is(result.Err, cleanup) {
		t.Fatalf("Run() error = %v, want both primary and cleanup failures", result.Err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("Run() exit code = %d, want primary command status 1", result.ExitCode)
	}
}

func TestRequiredInteractionFailsBeforeSideEffects(t *testing.T) {
	t.Parallel()

	called := false
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithInteraction(cli.InteractionRequired),
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			called = true
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{NonInteractive: true})
	if !errors.Is(result.Err, cli.ErrUsage) || called {
		t.Fatalf("Run() = (%v, called %t), want usage failure before handler", result.Err, called)
	}
}

func TestCancellationBetweenLifecyclePhasesStopsSideEffects(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"validation", "middleware", "pre", "run", "post"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()

			cause := errors.New("canceled during " + phase)
			ctx, cancel := context.WithCancelCause(context.Background())
			var events []string
			stop := func(current string) {
				events = append(events, current)
				if current == phase {
					cancel(cause)
				}
			}
			application, err := cli.Compile(cli.NewCommand(
				"tool",
				cli.WithValidation(func(context.Context, cli.Input) error {
					stop("validation")
					return nil
				}),
				cli.WithMiddleware(func(nextContext context.Context, _ cli.CommandMetadata, next cli.Next) error {
					stop("middleware")
					return next(nextContext)
				}),
				cli.WithPreRun(func(context.Context, cli.Invocation) error {
					stop("pre")
					return nil
				}),
				cli.WithHandler(func(context.Context, cli.Invocation) error {
					stop("run")
					return nil
				}),
				cli.WithPostRun(func(context.Context, cli.Invocation) error {
					stop("post")
					return nil
				}),
				cli.WithCleanup(func(context.Context, cli.Invocation) error {
					events = append(events, "cleanup")
					return nil
				}),
			))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}

			result := application.Run(ctx, cli.Request{})
			if !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, cause) {
				t.Fatalf("Run() error = %v, want cancellation cause", result.Err)
			}
			if result.ExitCode != 130 {
				t.Fatalf("Run() exit code = %d, want 130", result.ExitCode)
			}
			wantEvents := map[string][]string{
				"validation": {"validation"},
				"middleware": {"validation", "middleware", "cleanup"},
				"pre":        {"validation", "middleware", "pre", "cleanup"},
				"run":        {"validation", "middleware", "pre", "run", "cleanup"},
				"post":       {"validation", "middleware", "pre", "run", "post", "cleanup"},
			}[phase]
			if !reflect.DeepEqual(events, wantEvents) {
				t.Fatalf("events = %v, want %v", events, wantEvents)
			}
		})
	}
}

func TestMiddlewareNextRunsAtMostOnce(t *testing.T) {
	t.Parallel()

	runs := 0
	cleanups := 0
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithMiddleware(func(ctx context.Context, _ cli.CommandMetadata, next cli.Next) error {
			if err := next(ctx); err != nil {
				return err
			}
			return next(ctx)
		}),
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			runs++
			return nil
		}),
		cli.WithCleanup(func(context.Context, cli.Invocation) error {
			cleanups++
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result := application.Run(context.Background(), cli.Request{})
	if !errors.Is(result.Err, cli.ErrInternal) || runs != 1 || cleanups != 1 {
		t.Fatalf("Run() = (%v, runs %d, cleanups %d), want internal error and one execution", result.Err, runs, cleanups)
	}
}

func TestMiddlewareCannotContinueAfterReturning(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	continued := make(chan error, 1)
	runs := 0
	cleanups := 0
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithMiddleware(func(ctx context.Context, _ cli.CommandMetadata, next cli.Next) error {
			go func() {
				<-release
				continued <- next(ctx)
			}()
			return nil
		}),
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			runs++
			return nil
		}),
		cli.WithCleanup(func(context.Context, cli.Invocation) error {
			cleanups++
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result := application.Run(context.Background(), cli.Request{})
	close(release)
	continueErr := <-continued
	if result.Err != nil || !errors.Is(continueErr, cli.ErrInternal) {
		t.Fatalf("Run() error = %v, late Next() error = %v", result.Err, continueErr)
	}
	if runs != 0 || cleanups != 1 {
		t.Fatalf("runs = %d, cleanups = %d, want no late run and one cleanup", runs, cleanups)
	}
}

func TestLifecycleContextErrorsRemainDeterministic(t *testing.T) {
	t.Parallel()

	t.Run("validation cancellation error", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("validation canceled")
		ctx, cancel := context.WithCancelCause(context.Background())
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithValidation(func(context.Context, cli.Input) error {
				cancel(cause)
				return ctx.Err()
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		result := application.Run(ctx, cli.Request{})
		if !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, cause) {
			t.Fatalf("Run() error = %v, want validation cancellation", result.Err)
		}
	})

	t.Run("specific failure wins cancellation", func(t *testing.T) {
		t.Parallel()
		primary := errors.New("specific failure")
		ctx, cancel := context.WithCancelCause(context.Background())
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithHandler(func(context.Context, cli.Invocation) error {
				cancel(errors.New("secondary cancellation"))
				return primary
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		result := application.Run(ctx, cli.Request{})
		if !errors.Is(result.Err, primary) || errors.Is(result.Err, cli.ErrCanceled) {
			t.Fatalf("Run() error = %v, want specific primary failure", result.Err)
		}
	})

	t.Run("nested middleware rejects nil context", func(t *testing.T) {
		t.Parallel()
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithMiddleware(
				func(_ context.Context, _ cli.CommandMetadata, next cli.Next) error {
					return next(nil)
				},
				func(ctx context.Context, _ cli.CommandMetadata, next cli.Next) error {
					return next(ctx)
				},
			),
		))
		if err != nil {
			t.Fatal(err)
		}
		if result := application.Run(context.Background(), cli.Request{}); !errors.Is(result.Err, cli.ErrInternal) {
			t.Fatalf("Run() error = %v, want nil-context rejection", result.Err)
		}
	})

	t.Run("nested middleware rejects canceled context", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("middleware canceled")
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithMiddleware(
				func(_ context.Context, _ cli.CommandMetadata, next cli.Next) error {
					ctx, cancel := context.WithCancelCause(context.Background())
					cancel(cause)
					return next(ctx)
				},
				func(ctx context.Context, _ cli.CommandMetadata, next cli.Next) error {
					return next(ctx)
				},
			),
		))
		if err != nil {
			t.Fatal(err)
		}
		result := application.Run(context.Background(), cli.Request{})
		if !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, cause) {
			t.Fatalf("Run() error = %v, want middleware cancellation", result.Err)
		}
	})

	t.Run("middleware cannot swallow continued cancellation", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("continued context canceled")
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithMiddleware(func(_ context.Context, _ cli.CommandMetadata, next cli.Next) error {
				ctx, cancel := context.WithCancelCause(context.Background())
				cancel(cause)
				_ = next(ctx)
				return nil
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		result := application.Run(context.Background(), cli.Request{})
		if !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, cause) {
			t.Fatalf("Run() error = %v, want continued cancellation", result.Err)
		}
	})
}

type lifecycleContextKey struct{}
