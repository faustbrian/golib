package instant_test

import (
	"sync"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestImmutableSetSupportsConcurrentSharedReads(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	periods := make([]instant.Period, 256)
	for index := range periods {
		start := base.Add(time.Duration(index) * time.Minute)
		periods[index], _ = instant.Range(start, start.Add(30*time.Second))
	}
	set, err := instant.NewSet(temporal.DefaultLimits(), periods...)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wait.Add(1)
		go func(offset int) {
			defer wait.Done()
			for iteration := 0; iteration < 1_000; iteration++ {
				_ = set.Includes(base.Add(time.Duration(offset+iteration%256) * time.Minute))
				_ = set.Periods()
				_ = set.Gaps()
			}
		}(worker)
	}
	wait.Wait()
}
