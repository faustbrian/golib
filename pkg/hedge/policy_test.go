package hedge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

type staticClassifier struct{}

func (staticClassifier) Classify(_ context.Context, result hedge.AttemptResult[string]) (hedge.Classification, error) {
	if result.Err == nil {
		return hedge.ClassificationSuccess, nil
	}
	return hedge.ClassificationFailure, nil
}

type unlimitedBudget struct{}

func (unlimitedBudget) Capacity() uint { return 64 }

func (unlimitedBudget) TryAcquire(string) (hedge.Permit, bool) {
	return releaseFunc(func() {}), true
}

type releaseFunc func()

func (release releaseFunc) Release() { release() }

func validConfig() hedge.Config[string] {
	return hedge.Config[string]{
		MaxHedges:      1,
		ReplaySafe:     true,
		Delay:          time.Millisecond,
		TotalTimeout:   time.Second,
		CleanupTimeout: time.Second,
		Clock:          hedge.RealClock{},
		Budget:         unlimitedBudget{},
		Classifier:     staticClassifier{},
		Disposer: hedge.DisposeFunc[string](func(context.Context, string) error {
			return nil
		}),
		Resource:           "inventory-read",
		FactoryFailureMode: hedge.FactoryFailureStop,
	}
}

func TestNewPolicyRejectsImplicitOrUnboundedHedging(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*hedge.Config[string]){
		"zero value":          func(config *hedge.Config[string]) { *config = hedge.Config[string]{} },
		"no hedges":           func(config *hedge.Config[string]) { config.MaxHedges = 0 },
		"replay not declared": func(config *hedge.Config[string]) { config.ReplaySafe = false },
		"no delay":            func(config *hedge.Config[string]) { config.Delay = 0 },
		"negative delay":      func(config *hedge.Config[string]) { config.Delay = -time.Second },
		"no total timeout":    func(config *hedge.Config[string]) { config.TotalTimeout = 0 },
		"negative timeout":    func(config *hedge.Config[string]) { config.TotalTimeout = -time.Second },
		"no cleanup timeout":  func(config *hedge.Config[string]) { config.CleanupTimeout = 0 },
		"no clock":            func(config *hedge.Config[string]) { config.Clock = nil },
		"no budget":           func(config *hedge.Config[string]) { config.Budget = nil },
		"zero budget":         func(config *hedge.Config[string]) { config.Budget = zeroCapacityBudget{} },
		"panicking budget":    func(config *hedge.Config[string]) { config.Budget = panicCapacityBudget{} },
		"no classifier":       func(config *hedge.Config[string]) { config.Classifier = nil },
		"no disposer":         func(config *hedge.Config[string]) { config.Disposer = nil },
		"no resource":         func(config *hedge.Config[string]) { config.Resource = "" },
		"no factory mode":     func(config *hedge.Config[string]) { config.FactoryFailureMode = 0 },
		"too many hedges":     func(config *hedge.Config[string]) { config.MaxHedges = hedge.MaxHedges + 1 },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := validConfig()
			mutate(&config)
			if _, err := hedge.NewPolicy(config); !errors.Is(err, hedge.ErrInvalidPolicy) {
				t.Fatalf("NewPolicy() error = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

type zeroCapacityBudget struct{}

func (zeroCapacityBudget) Capacity() uint                         { return 0 }
func (zeroCapacityBudget) TryAcquire(string) (hedge.Permit, bool) { return nil, false }

type panicCapacityBudget struct{}

func (panicCapacityBudget) Capacity() uint                         { panic("private") }
func (panicCapacityBudget) TryAcquire(string) (hedge.Permit, bool) { return nil, false }

func TestNewPolicyAcceptsExplicitBoundedConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := hedge.NewPolicy(validConfig()); err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
}
