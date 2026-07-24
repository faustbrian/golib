//go:build go1.1

package consumer

import (
	"secretmodel"
	"sinkapi"
)

func BuildTaggedSensitive(token secretmodel.Token) {
	sinkapi.Record(token) // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.Record argument 1`
}
