package hedge_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestNoPackageBackgroundWorkers(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 20 {
		config := validConfig()
		config.Delay = time.Microsecond
		policy, err := hedge.NewPolicy(config)
		if err != nil {
			t.Fatal(err)
		}
		factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
			return func(context.Context) (string, error) { return "value", nil }, "pod", nil
		})
		_, report, err := hedge.Do(context.Background(), policy, factory)
		if err != nil {
			t.Fatal(err)
		}
		if err := report.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines before=%d after=%d", before, after)
	}
}
