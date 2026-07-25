package window_test

import (
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func ExampleCount() {
	recent, _ := window.NewCount(2)
	_ = recent.Add(window.Record{Class: window.Success})
	_ = recent.Add(window.Record{Class: window.Failure, Slow: true})
	_ = recent.Add(window.Record{Class: window.Ignored})
	snapshot := recent.Snapshot()
	fmt.Println(snapshot.Classified, snapshot.Failures, snapshot.SlowFailure, snapshot.Ignored)
	// Output: 2 1 1 1
}

func ExampleTime() {
	recent, _ := window.NewTime(time.Second, 2)
	start := time.Unix(100, 0)
	_ = recent.Add(start, window.Record{Class: window.Failure})
	fmt.Println(recent.Snapshot(start).Failures)
	fmt.Println(recent.Snapshot(start.Add(2 * time.Second)).Failures)
	// Output:
	// 1
	// 0
}
