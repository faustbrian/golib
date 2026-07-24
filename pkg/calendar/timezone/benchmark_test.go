package timezone_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func BenchmarkResolveLocalDateTime(b *testing.B) {
	location, err := calendartz.LoadLocation("America/New_York")
	if err != nil {
		b.Fatal(err)
	}
	local := calendartz.MustLocalDateTime(calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0)
	for b.Loop() {
		_, _ = calendartz.Resolve(local, location, calendartz.Earlier)
	}
}
