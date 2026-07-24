package consumer

import (
	"secretmodel"
	. "sinkapi"
)

func DotImport(token secretmodel.Token) {
	Record(token) // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.Record argument 1`
}
