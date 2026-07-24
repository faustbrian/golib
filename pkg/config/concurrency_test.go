package config_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configtest"
)

func TestPlanLoadsDeterministicallyInParallel(t *testing.T) {
	t.Parallel()

	type settings struct {
		Name   string            `config:"name"`
		Labels map[string]string `config:"labels"`
	}
	source := configtest.NewSource(
		config.SourceInfo{Name: "parallel", Priority: config.PriorityExplicitFiles},
		config.Document{Tree: map[string]any{
			"name":   "worker",
			"labels": map[string]any{"region": "eu"},
		}},
	)
	plan := configtest.MustPlan(t, source)

	const loads = 128
	failures := make(chan error, loads)
	var group sync.WaitGroup
	group.Add(loads)
	for index := range loads {
		go func() {
			defer group.Done()
			snapshot, err := config.Load[settings](context.Background(), plan)
			if err != nil {
				failures <- err
				return
			}
			value := snapshot.Value()
			if value.Name != "worker" || value.Labels["region"] != "eu" {
				failures <- fmt.Errorf("load %d returned %#v", index, value)
				return
			}
			value.Labels["region"] = "mutated"
			if snapshot.Value().Labels["region"] != "eu" {
				failures <- fmt.Errorf("load %d exposed snapshot mutation", index)
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestConcurrentPlansHooksValidatorsSnapshotsAndFailures(t *testing.T) {
	t.Parallel()

	type settings struct {
		Name   concurrentText       `config:"name"`
		Labels map[string]string    `config:"labels"`
		Count  config.Optional[int] `config:"count"`
	}
	sharedSource := configtest.NewSource(
		config.SourceInfo{Name: "shared", Priority: config.PriorityExplicitFiles},
		config.Document{
			Tree: map[string]any{
				"name": "worker", "labels": map[string]any{"region": "eu"}, "count": int64(0),
			},
			Origins: map[string]config.Origin{
				"name": {Source: "shared", Present: true, State: config.Present},
			},
		},
	)
	sharedPlan := configtest.MustPlan(t, sharedSource)
	sharedSnapshot, err := config.Load[settings](context.Background(), sharedPlan)
	if err != nil {
		t.Fatalf("Load() shared snapshot error = %v", err)
	}

	sourceFailure := errors.New("source failure")
	failingPlan := configtest.MustPlan(t, source{
		info: config.SourceInfo{Name: "failing"}, err: sourceFailure,
	})
	decodePlan := configtest.MustPlan(t, source{
		info: config.SourceInfo{Name: "decode"}, tree: map[string]any{"name": []any{}},
	})

	const workers = 96
	failures := make(chan error, workers*6)
	var group sync.WaitGroup
	group.Add(workers)
	for index := range workers {
		go func() {
			defer group.Done()

			plan, err := config.NewPlan(sharedSource)
			if err != nil {
				failures <- fmt.Errorf("worker %d plan: %w", index, err)
				return
			}
			snapshot, err := config.LoadWithValidators(
				context.Background(),
				plan,
				func(_ context.Context, value settings) error {
					if value.Name != "worker" || value.Count.State() != config.Present {
						return errors.New("unexpected candidate")
					}
					return nil
				},
			)
			if err != nil {
				failures <- fmt.Errorf("worker %d validated load: %w", index, err)
				return
			}
			value := snapshot.Value()
			value.Labels["region"] = "mutated"
			if snapshot.Value().Labels["region"] != "eu" {
				failures <- fmt.Errorf("worker %d mutated local snapshot", index)
			}

			shared := sharedSnapshot.Value()
			shared.Labels["region"] = "mutated"
			if sharedSnapshot.Value().Labels["region"] != "eu" {
				failures <- fmt.Errorf("worker %d mutated shared snapshot", index)
			}
			if origin, ok := sharedSnapshot.Origin("name"); !ok || origin.Source != "shared" {
				failures <- fmt.Errorf("worker %d origin = %#v, %v", index, origin, ok)
			}

			if failed, err := config.Load[settings](context.Background(), failingPlan); failed != nil || !errors.Is(err, sourceFailure) {
				failures <- fmt.Errorf("worker %d source failure snapshot = %#v: %w", index, failed, err)
			}
			if failed, err := config.Load[settings](context.Background(), decodePlan); failed != nil || err == nil {
				failures <- fmt.Errorf("worker %d decode failure snapshot = %#v: %w", index, failed, err)
			}
			if failed, err := config.LoadWithValidators(
				context.Background(),
				sharedPlan,
				func(context.Context, settings) error { return errors.New("validation failure") },
			); failed != nil || err == nil {
				failures <- fmt.Errorf("worker %d validation failure snapshot = %#v: %w", index, failed, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if failed, err := config.Load[settings](ctx, sharedPlan); failed != nil || !errors.Is(err, context.Canceled) {
				failures <- fmt.Errorf("worker %d cancellation snapshot = %#v: %w", index, failed, err)
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

type concurrentText string

func (value *concurrentText) UnmarshalText(text []byte) error {
	*value = concurrentText(string(text))
	return nil
}
