package queueservice

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
)

const processHelperEnvironment = "QUEUE_SERVICE_PROCESS_HELPER"

type durableProcessState struct {
	Effects int  `json:"effects"`
	Acked   bool `json:"acked"`
}

func TestProcessTerminationDuplicateWindowAndRecovery(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		phase       string
		wantEffects int
	}{
		{name: "before-handler-effect", mode: "before-effect", phase: "before-effect", wantEffects: 1},
		{name: "after-handler-effect", mode: "after-effect", phase: "after-effect", wantEffects: 2},
		{name: "after-settlement", mode: "after-settlement", phase: "after-settlement", wantEffects: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "durable-state.json")
			writeDurableProcessState(t, statePath, durableProcessState{})
			killProcessAtPhase(t, statePath, test.mode, test.phase)
			if err := os.Remove(statePath + ".lease"); err != nil {
				t.Fatalf("expire killed process lease: %v", err)
			}
			runRecoveryProcesses(t, statePath, 2)

			state := readDurableProcessState(t, statePath)
			if !state.Acked || state.Effects != test.wantEffects {
				t.Fatalf(
					"recovered state = %+v, want acked with %d effect(s)",
					state,
					test.wantEffects,
				)
			}
		})
	}
}

func TestQueueServiceProcessHelper(t *testing.T) {
	mode := os.Getenv(processHelperEnvironment)
	if mode == "" {
		return
	}
	statePath := os.Getenv("QUEUE_SERVICE_PROCESS_STATE")
	leasePath := statePath + ".lease"
	if err := os.Mkdir(leasePath, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			writeProcessPhase(t, "no-work")

			return
		}
		t.Fatalf("acquire durable lease: %v", err)
	}
	defer func() {
		if err := os.Remove(leasePath); err != nil {
			t.Errorf("release durable lease: %v", err)
		}
	}()
	state := readDurableProcessState(t, statePath)
	if state.Acked {
		writeProcessPhase(t, "no-work")

		return
	}

	completed := make(chan struct{})
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[string]{
		Name: "process-worker", Resource: statePath, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error {
			if mode == "before-effect" {
				writeProcessPhase(t, "before-effect")
				select {}
			}
			state := readDurableProcessState(t, statePath)
			state.Effects++
			writeDurableProcessState(t, statePath, state)
			if mode == "after-effect" {
				writeProcessPhase(t, "after-effect")
				select {}
			}

			return nil
		},
		Run: func(ctx context.Context, _ string, handler Handler) error {
			if err := handler(ctx, plainTask("durable-work")); err != nil {
				return err
			}
			state := readDurableProcessState(t, statePath)
			state.Acked = true
			writeDurableProcessState(t, statePath, state)
			if mode == "after-settlement" {
				writeProcessPhase(t, "after-settlement")
				select {}
			}
			writeProcessPhase(t, "settled")
			close(completed)
			<-ctx.Done()

			return nil
		},
		Shutdown: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	plan := worker.Plan()
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runContext, cancelRun := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- plan.Tasks[0].Run(runContext) }()
	<-completed
	cancelRun()
	if err = <-runResult; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func killProcessAtPhase(t *testing.T, statePath, mode, phase string) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestQueueServiceProcessHelper$")
	command.Env = append(os.Environ(),
		processHelperEnvironment+"="+mode,
		"QUEUE_SERVICE_PROCESS_STATE="+statePath,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout pipe: %v", err)
	}
	command.Stderr = command.Stdout
	if err = command.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	phaseSeen := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == phase {
				phaseSeen <- true

				return
			}
		}
		phaseSeen <- false
	}()
	select {
	case seen := <-phaseSeen:
		if !seen {
			t.Fatal("helper exited before the requested termination phase")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not reach the requested termination phase")
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatalf("kill helper process: %v", err)
	}
	if err = command.Wait(); err == nil {
		t.Fatal("killed helper process exited successfully")
	}
}

func runRecoveryProcesses(t *testing.T, statePath string, count int) {
	t.Helper()
	var group sync.WaitGroup
	errorsFound := make(chan error, count)
	for process := 0; process < count; process++ {
		group.Add(1)
		go func() {
			defer group.Done()
			command := exec.Command(os.Args[0], "-test.run=^TestQueueServiceProcessHelper$")
			command.Env = append(os.Environ(),
				processHelperEnvironment+"=recover",
				"QUEUE_SERVICE_PROCESS_STATE="+statePath,
			)
			output, err := command.CombinedOutput()
			if err != nil {
				errorsFound <- fmt.Errorf("recovery helper: %w: %s", err, output)
			}
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
}

func readDurableProcessState(t *testing.T, path string) durableProcessState {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read durable process state: %v", err)
	}
	state := durableProcessState{}
	if err = json.Unmarshal(encoded, &state); err != nil {
		t.Fatalf("decode durable process state: %v", err)
	}

	return state
}

func writeDurableProcessState(t *testing.T, path string, state durableProcessState) {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode durable process state: %v", err)
	}
	temporary := path + ".tmp"
	if err = os.WriteFile(temporary, encoded, 0o600); err != nil {
		t.Fatalf("write durable process state: %v", err)
	}
	if err = os.Rename(temporary, path); err != nil {
		t.Fatalf("commit durable process state: %v", err)
	}
}

func writeProcessPhase(t *testing.T, phase string) {
	t.Helper()
	if _, err := fmt.Fprintln(os.Stdout, phase); err != nil {
		t.Fatalf("write process phase: %v", err)
	}
}
