package prompts_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestTaskGroupPreservesExplicitOwnershipAndOrder(t *testing.T) {
	t.Parallel()

	group, err := prompts.NewTaskGroup(prompts.TaskGroupConfig{ID: "deploy", Label: "Deploy", MaxTasks: 4})
	if err != nil {
		t.Fatalf("NewTaskGroup() error = %v", err)
	}
	build, err := group.Add(prompts.TaskConfig{ID: "build", Label: "Build", Total: 10})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	upload, err := group.Add(prompts.TaskConfig{ID: "upload", Label: "Upload", ParentID: "build"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := build.Update(10, "built"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	build.Complete("done")
	if err := upload.Increment(3, "sending"); err != nil {
		t.Fatalf("Increment() error = %v", err)
	}
	upload.Fail("network")

	snapshots := group.Snapshot()
	if len(snapshots) != 2 || snapshots[0].ID != "build" || snapshots[1].ID != "upload" || snapshots[1].ParentID != "build" {
		t.Fatalf("Snapshot() = %#v", snapshots)
	}
	snapshots[0].Message = "changed"
	if group.Snapshot()[0].Message != "done" {
		t.Fatal("Snapshot() exposed group state")
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	if err := group.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	output := terminal.Output()
	if !strings.Contains(output, "Deploy") || !strings.Contains(output, "success: Build: 10/10") ||
		!strings.Contains(output, "error:   Upload: 3 - network") {
		t.Fatalf("task output = %q", output)
	}
}

func TestTaskGroupConcurrentUpdatesDoNotCreateWorkers(t *testing.T) {
	t.Parallel()

	group, err := prompts.NewTaskGroup(prompts.TaskGroupConfig{ID: "work", Label: "Work", MaxTasks: 2})
	if err != nil {
		t.Fatalf("NewTaskGroup() error = %v", err)
	}
	task, err := group.Add(prompts.TaskConfig{ID: "items", Label: "Items"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	var wait sync.WaitGroup
	for range 100 {
		wait.Go(func() { _ = task.Increment(1, "item") })
	}
	wait.Wait()
	if snapshot := task.Snapshot(); snapshot.Current != 100 || snapshot.State != prompts.ProgressRunning {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	if group.Len() != 1 {
		t.Fatalf("Len() = %d", group.Len())
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	if err := group.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil || !strings.Contains(terminal.Output(), "progress: Items") {
		t.Fatalf("running Render() = %v, output %q", err, terminal.Output())
	}
	task.Cancel("stopped")
	terminal = prompts.NewVirtualTerminal(40, 8)
	if err := group.Render(context.Background(), prompts.Execution{Output: terminal}); err != nil || !strings.Contains(terminal.Output(), "warning: Items") {
		t.Fatalf("canceled Render() = %v, output %q", err, terminal.Output())
	}
}

func TestTaskGroupRejectsInvalidDefinitionsAndOwnership(t *testing.T) {
	t.Parallel()

	for _, config := range []prompts.TaskGroupConfig{{}, {ID: "group"}, {ID: "group", Label: "Group", MaxTasks: -1}} {
		if _, err := prompts.NewTaskGroup(config); !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewTaskGroup(%#v) error = %v", config, err)
		}
	}
	group, err := prompts.NewTaskGroup(prompts.TaskGroupConfig{ID: "group", Label: "Group", MaxTasks: 1})
	if err != nil {
		t.Fatalf("NewTaskGroup() error = %v", err)
	}
	if _, err := group.Add(prompts.TaskConfig{ID: "child", Label: "Child", ParentID: "missing"}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("missing parent error = %v", err)
	}
	if _, err := group.Add(prompts.TaskConfig{ID: "one", Label: "One", Total: -1}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid task error = %v", err)
	}
	if _, err := group.Add(prompts.TaskConfig{ID: "one", Label: "One"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := group.Add(prompts.TaskConfig{ID: "one", Label: "Duplicate"}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("duplicate task error = %v", err)
	}
	if _, err := group.Add(prompts.TaskConfig{ID: "two", Label: "Two"}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("capacity error = %v", err)
	}
}
