// Package encoding exposes the canonical schedule wire contract without
// shadowing human-readable formatting concerns in the root package.
package encoding

import openinghours "github.com/faustbrian/golib/pkg/opening-hours"

// Marshal returns canonical versioned JSON.
func Marshal(schedule openinghours.Schedule) ([]byte, error) {
	return schedule.CanonicalJSON()
}

// Unmarshal strictly parses canonical JSON into an immutable schedule.
func Unmarshal(data []byte) (openinghours.Schedule, error) {
	return openinghours.ParseJSON(data)
}
