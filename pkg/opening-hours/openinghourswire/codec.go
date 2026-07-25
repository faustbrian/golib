// Package openinghourswire adapts schedules to byte-oriented wire registries.
package openinghourswire

import (
	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	wire "github.com/faustbrian/golib/pkg/wire"
)

// Format is the stable registry name for canonical opening-hours JSON.
const Format = "opening-hours+json;v=1"

// WireFormat is the typed wire registry identity.
const WireFormat wire.Format = Format

// Codec is stateless and safe for concurrent use.
type Codec struct{}

// Encode returns canonical bytes.
func (Codec) Encode(schedule openinghours.Schedule) ([]byte, error) {
	return schedule.CanonicalJSON()
}

// Decode strictly parses canonical bytes.
func (Codec) Decode(data []byte) (openinghours.Schedule, error) {
	return openinghours.ParseJSON(data)
}
