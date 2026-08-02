package resilience_test

import "time"

const exampleDuration = time.Minute

type fixedExampleClock struct{}

func (fixedExampleClock) Now() time.Time { return time.Unix(1, 0) }
