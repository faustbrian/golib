package prompts

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TaskGroupConfig defines explicit ordered task presentation ownership.
type TaskGroupConfig struct {
	ID, Label string
	MaxTasks  int
}

// TaskConfig defines one caller-owned task state.
type TaskConfig struct {
	ID, Label       string
	ParentID        string
	Total           int64
	AllowRegression bool
}

// TaskSnapshot is a progress snapshot with explicit parent ownership.
type TaskSnapshot struct {
	ProgressSnapshot
	ParentID string
}

// Task presents caller-owned work; it never schedules or retries that work.
type Task struct {
	*Progress
	parentID string
}

// Snapshot returns a stable task state copy.
func (task *Task) Snapshot() TaskSnapshot {
	return TaskSnapshot{ProgressSnapshot: task.Progress.Snapshot(), ParentID: task.parentID}
}

// TaskGroup preserves Add order and explicit nesting.
type TaskGroup struct {
	mu       sync.RWMutex
	id       string
	label    string
	maximum  int
	tasks    []*Task
	identity map[string]*Task
}

// NewTaskGroup creates an ordered bounded presentation group.
func NewTaskGroup(config TaskGroupConfig) (*TaskGroup, error) {
	maximum := config.MaxTasks
	if maximum == 0 {
		maximum = 100
	}
	if config.ID == "" || config.Label == "" || maximum < 1 {
		return nil, invalidBehaviorDefinition("define task group", config.ID, ErrInvalidDefinition)
	}
	return &TaskGroup{
		id: config.ID, label: config.Label, maximum: maximum,
		tasks: make([]*Task, 0, maximum), identity: make(map[string]*Task, maximum),
	}, nil
}

// Add declares a task after its optional parent and returns its state handle.
func (group *TaskGroup) Add(config TaskConfig) (*Task, error) {
	group.mu.Lock()
	defer group.mu.Unlock()
	if _, duplicate := group.identity[config.ID]; duplicate {
		return nil, invalidBehaviorDefinition("add task", config.ID, fmt.Errorf("%w: duplicate task identity", ErrInvalidDefinition))
	}
	if len(group.tasks) >= group.maximum {
		return nil, invalidBehaviorDefinition("add task", config.ID, fmt.Errorf("%w: task capacity reached", ErrInvalidDefinition))
	}
	if config.ParentID != "" {
		if _, exists := group.identity[config.ParentID]; !exists {
			return nil, invalidBehaviorDefinition("add task", config.ID, fmt.Errorf("%w: task parent must already exist", ErrInvalidDefinition))
		}
	}
	progress, err := NewProgress(ProgressConfig{
		ID: config.ID, Label: config.Label, Total: config.Total,
		AllowRegression: config.AllowRegression,
	})
	if err != nil {
		return nil, err
	}
	task := &Task{Progress: progress, parentID: config.ParentID}
	group.tasks = append(group.tasks, task)
	group.identity[config.ID] = task
	return task, nil
}

// Len returns the declared task count.
func (group *TaskGroup) Len() int {
	group.mu.RLock()
	defer group.mu.RUnlock()
	return len(group.tasks)
}

// Snapshot returns task states in Add order.
func (group *TaskGroup) Snapshot() []TaskSnapshot {
	group.mu.RLock()
	defer group.mu.RUnlock()
	result := make([]TaskSnapshot, len(group.tasks))
	for index, task := range group.tasks {
		result[index] = task.Snapshot()
	}
	return result
}

// Render writes a deterministic nested task summary in Add order.
func (group *TaskGroup) Render(ctx context.Context, execution Execution) error {
	snapshots := group.Snapshot()
	lines := make([]SemanticLine, 0, len(snapshots)+1)
	lines = append(lines, Line(Text(RoleLabel, group.label)))
	parents := make(map[string]string, len(snapshots))
	for _, snapshot := range snapshots {
		parents[snapshot.ID] = snapshot.ParentID
		depth := 0
		for parent := snapshot.ParentID; parent != ""; parent = parents[parent] {
			depth++
		}
		role := RoleProgress
		switch snapshot.State {
		case ProgressPending, ProgressRunning:
			role = RoleProgress
		case ProgressSucceeded:
			role = RoleSuccess
		case ProgressFailed:
			role = RoleError
		case ProgressCanceled:
			role = RoleWarning
		}
		text := fmt.Sprintf("%s%s: %d", strings.Repeat("  ", depth), snapshot.Label, snapshot.Current)
		if snapshot.Total > 0 {
			text += fmt.Sprintf("/%d", snapshot.Total)
		}
		if snapshot.Message != "" {
			text += " - " + snapshot.Message
		}
		lines = append(lines, Line(Text(role, text)))
	}
	return renderOutput(ctx, group.id, NewFrame(lines...), execution)
}
