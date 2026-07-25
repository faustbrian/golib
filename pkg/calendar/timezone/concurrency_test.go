package timezone_test

import (
	"sync"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func TestConcurrentTimezoneConversion(t *testing.T) {
	t.Parallel()

	location, err := calendartz.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	local := calendartz.MustLocalDateTime(calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0)
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				instant, err := calendartz.Resolve(local, location, calendartz.Earlier)
				if err != nil {
					t.Errorf("Resolve: %v", err)
					return
				}
				if _, offset := instant.Zone(); offset != -4*60*60 {
					t.Errorf("offset = %d", offset)
					return
				}
			}
		}()
	}
	wait.Wait()
}
