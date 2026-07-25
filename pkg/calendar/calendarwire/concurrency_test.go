package calendarwire_test

import (
	"sync"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/calendarwire"
)

func TestWireCodecConcurrentUse(t *testing.T) {
	t.Parallel()

	date := calendar.MustDate(2024, time.February, 29)
	payload := []byte(`"2024-02-29"`)
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				encoded, err := calendarwire.EncodeDate(date)
				if err != nil || string(encoded) != string(payload) {
					t.Errorf("EncodeDate = %q, %v", encoded, err)
					return
				}
				decoded, err := calendarwire.DecodeDate(payload)
				if err != nil || decoded != date {
					t.Errorf("DecodeDate = %s, %v", decoded, err)
					return
				}
			}
		}()
	}
	wait.Wait()
}
