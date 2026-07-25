package postgres_test

import (
	"sync"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
)

func TestPostgreSQLCodecValuesConcurrentReadUse(t *testing.T) {
	t.Parallel()

	finite := calendarpg.NewDate(calendar.MustDate(2024, time.February, 29))
	infinity := calendarpg.NewInfinityDate(calendarpg.PositiveInfinity)
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				if value, err := finite.Value(); err != nil || value != "2024-02-29" {
					t.Errorf("finite Value = %v, %v", value, err)
					return
				}
				if value, err := finite.DateValue(); err != nil || !value.Valid {
					t.Errorf("finite DateValue = %#v, %v", value, err)
					return
				}
				if value, err := infinity.Value(); err != nil || value != "infinity" {
					t.Errorf("infinity Value = %v, %v", value, err)
					return
				}
			}
		}()
	}
	wait.Wait()
}
