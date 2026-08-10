//go:build integration

package postgres_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	lifecycleHelperEnv         = "EVENT_SOURCING_POSTGRES_LIFECYCLE_HELPER"
	lifecycleDSNEnv            = "EVENT_SOURCING_POSTGRES_LIFECYCLE_DSN"
	lifecycleOldMode           = "old"
	lifecycleReplacementMode   = "replacement"
	lifecycleProjection        = "lifecycle-account-summary"
	lifecycleApplicationPrefix = "event-sourcing-lifecycle-"
)

func TestPostgreSQLSIGTERMDrainsBeforeReplacementReadiness(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM process semantics are unavailable on Windows")
	}

	ctx, pool := newDerivedIntegrationPool(t)
	projectionStore, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := projectionStore.Save(
		ctx,
		lifecycleProjection,
		0,
		1,
	); err != nil {
		t.Fatal(err)
	}

	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blocked := true
	t.Cleanup(func() {
		if blocked {
			_ = blocker.Rollback(context.Background())
		}
	})
	var position int64
	if err := blocker.QueryRow(
		ctx,
		`SELECT last_position
		 FROM event_sourcing.positions
		 WHERE singleton = true
		 FOR UPDATE`,
	).Scan(&position); err != nil {
		t.Fatal(err)
	}
	if position != 0 {
		t.Fatalf("initial global position = %d", position)
	}
	var checkpoint int64
	if err := blocker.QueryRow(
		ctx,
		`SELECT checkpoint
		 FROM event_sourcing.projections
		 WHERE name = $1
		 FOR UPDATE`,
		lifecycleProjection,
	).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint != 1 {
		t.Fatalf("initial projection checkpoint = %d", checkpoint)
	}

	oldPod := startLifecycleHelper(t, pool, lifecycleOldMode)
	assertLifecycleReadinessEventually(t, oldPod.readyURL, http.StatusOK)
	waitForLifecycleBlockedSessions(t, ctx, pool, lifecycleOldMode, 2)
	if err := oldPod.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	assertLifecycleReadinessEventually(
		t,
		oldPod.readyURL,
		http.StatusServiceUnavailable,
	)

	replacementPod := startLifecycleHelper(t, pool, lifecycleReplacementMode)
	assertLifecycleReadinessEventually(
		t,
		replacementPod.readyURL,
		http.StatusServiceUnavailable,
	)
	waitForLifecycleBlockedSessions(
		t,
		ctx,
		pool,
		lifecycleReplacementMode,
		1,
	)
	assertLifecycleProcessRunning(t, oldPod)
	assertLifecycleProcessRunning(t, replacementPod)

	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	blocked = false
	oldPod.waitForCleanExit(t)
	assertLifecycleReadinessEventually(
		t,
		replacementPod.readyURL,
		http.StatusOK,
	)

	if err := replacementPod.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	replacementPod.waitForCleanExit(t)
	assertLifecycleDurableState(t, ctx, pool)
}

func TestPostgreSQLLifecycleHelperProcess(t *testing.T) {
	mode := os.Getenv(lifecycleHelperEnv)
	if mode == "" {
		return
	}
	if mode != lifecycleOldMode && mode != lifecycleReplacementMode {
		t.Fatalf("unknown lifecycle helper mode %q", mode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv(lifecycleDSNEnv))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["application_name"] =
		lifecycleApplicationPrefix + mode
	config.MaxConns = 3
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := runLifecycleHelper(ctx, pool, mode); err != nil {
		t.Fatal(err)
	}
}

type lifecycleHelperProcess struct {
	command     *exec.Cmd
	readyURL    string
	waitStarted bool
	waited      bool
}

func startLifecycleHelper(
	t testing.TB,
	pool *pgxpool.Pool,
	mode string,
) *lifecycleHelperProcess {
	t.Helper()

	command := exec.Command(
		os.Args[0],
		"-test.run=^TestPostgreSQLLifecycleHelperProcess$",
		"-test.count=1",
	)
	command.Env = append(
		os.Environ(),
		lifecycleHelperEnv+"="+mode,
		lifecycleDSNEnv+"="+pool.Config().ConnString(),
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := &lifecycleHelperProcess{command: command}
	t.Cleanup(func() {
		if process.waited {
			return
		}
		_ = command.Process.Kill()
		if !process.waitStarted {
			_ = command.Wait()
		}
	})

	readCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	address := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			readErr <- err
			return
		}
		address <- strings.TrimSpace(line)
	}()
	select {
	case readyURL := <-address:
		if !strings.HasPrefix(readyURL, "http://127.0.0.1:") {
			t.Fatalf(
				"%s helper readiness address = %q",
				mode,
				readyURL,
			)
		}
		process.readyURL = readyURL
	case err := <-readErr:
		t.Fatalf(
			"read %s helper readiness: %v",
			mode,
			err,
		)
	case <-readCtx.Done():
		t.Fatalf(
			"wait for %s helper readiness: %v",
			mode,
			readCtx.Err(),
		)
	}

	return process
}

func (process *lifecycleHelperProcess) waitForCleanExit(t testing.TB) {
	t.Helper()

	result := make(chan error, 1)
	process.waitStarted = true
	go func() { result <- process.command.Wait() }()
	select {
	case err := <-result:
		process.waited = true
		if err != nil {
			t.Fatalf("lifecycle helper exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("lifecycle helper did not exit before drain deadline")
	}
}

func assertLifecycleProcessRunning(
	t testing.TB,
	process *lifecycleHelperProcess,
) {
	t.Helper()

	if err := process.command.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("lifecycle helper exited while work was blocked: %v", err)
	}
}

func runLifecycleHelper(
	ctx context.Context,
	pool *pgxpool.Pool,
	mode string,
) error {
	var ready atomic.Bool
	ready.Store(mode == lifecycleOldMode)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			if ready.Load() {
				writer.WriteHeader(http.StatusOK)
				return
			}
			writer.WriteHeader(http.StatusServiceUnavailable)
		}),
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	if _, err := fmt.Fprintf(os.Stdout, "http://%s\n", listener.Addr()); err != nil {
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)

	var workErr error
	if mode == lifecycleOldMode {
		workErr = drainOldLifecyclePod(ctx, pool, signals, &ready)
	} else {
		workErr = reconcileReplacementLifecyclePod(ctx, pool, signals, &ready)
	}
	ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	serveErr := <-serveResult
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.Join(workErr, shutdownErr, serveErr)
	}

	return errors.Join(workErr, shutdownErr)
}

func drainOldLifecyclePod(
	ctx context.Context,
	pool *pgxpool.Pool,
	signals <-chan os.Signal,
	ready *atomic.Bool,
) error {
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		return err
	}
	projectionStore, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		return err
	}
	stream, pending := lifecycleAppendInput()
	results := make(chan error, 2)
	go func() {
		_, appendErr := store.Append(
			ctx,
			stream,
			eventsourcing.ExpectNewStream(),
			pending,
		)
		results <- appendErr
	}()
	go func() {
		results <- projectionStore.Save(ctx, lifecycleProjection, 1, 2)
	}()

	select {
	case <-signals:
		ready.Store(false)
	case <-ctx.Done():
		return ctx.Err()
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func reconcileReplacementLifecyclePod(
	ctx context.Context,
	pool *pgxpool.Pool,
	signals <-chan os.Signal,
	ready *atomic.Bool,
) error {
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		return err
	}
	projectionStore, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		return err
	}
	stream, pending := lifecycleAppendInput()
	messages, outcome, err := store.ReconcileAppend(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil {
		return err
	}
	if outcome != eventsourcing.CommitCommitted || len(messages) != 1 ||
		messages[0].ID() != pending[0].ID() {
		return fmt.Errorf(
			"replacement reconciliation = %d messages, outcome %d",
			len(messages),
			outcome,
		)
	}

	for {
		status, err := projectionStore.Status(ctx, lifecycleProjection)
		if err != nil {
			return err
		}
		checkpoint, exists := status.Checkpoint()
		if status.State() == projection.StateRunning && exists && checkpoint == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	ready.Store(true)

	select {
	case <-signals:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lifecycleAppendInput() (
	eventsourcing.StreamID,
	[]eventsourcing.PendingMessage,
) {
	stream, err := eventsourcing.NewStreamID("account", "lifecycle-account")
	if err != nil {
		panic(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte(`{"account_id":"lifecycle-account"}`),
		},
	)
	if err != nil {
		panic(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "lifecycle-account-opened",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		panic(err)
	}

	return stream, []eventsourcing.PendingMessage{pending}
}

func waitForLifecycleBlockedSessions(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	mode string,
	want int,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		err := pool.QueryRow(
			deadline,
			`SELECT count(*)
			 FROM pg_stat_activity
			 WHERE application_name = $1
				AND wait_event_type = 'Lock'`,
			lifecycleApplicationPrefix+mode,
		).Scan(&count)
		if err == nil && count == want {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for %s blocked sessions = %d, want %d: %v: %v",
				mode,
				count,
				want,
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}

func assertLifecycleReadinessEventually(
	t testing.TB,
	readyURL string,
	want int,
) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	client := &http.Client{Timeout: time.Second}
	var actual int
	var lastErr error
	for {
		request, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			readyURL+"/readyz",
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err == nil {
			actual = response.StatusCode
			lastErr = response.Body.Close()
			if actual == want && lastErr == nil {
				return
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"readiness status = %d, want %d: %v",
				actual,
				want,
				lastErr,
			)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func assertLifecycleDurableState(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	stream, pending := lifecycleAppendInput()
	var count, streamVersion, globalPosition int64
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*), max(stream_version), max(global_position)
		 FROM event_sourcing.messages
		 WHERE message_id = $1`,
		pending[0].ID().String(),
	).Scan(&count, &streamVersion, &globalPosition); err != nil {
		t.Fatal(err)
	}
	if count != 1 || streamVersion != 1 || globalPosition != 1 {
		t.Fatalf(
			"durable message state = %d/%d/%d",
			count,
			streamVersion,
			globalPosition,
		)
	}
	var head, checkpoint int64
	if err := pool.QueryRow(
		ctx,
		`SELECT current_version
		 FROM event_sourcing.streams
		 WHERE aggregate_type = $1 AND aggregate_id = $2`,
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&head); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT checkpoint
		 FROM event_sourcing.projections
		 WHERE name = $1`,
		lifecycleProjection,
	).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if head != 1 || checkpoint != 2 {
		t.Fatalf("durable stream/projection state = %d/%d", head, checkpoint)
	}
}
