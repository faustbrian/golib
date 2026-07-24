package approved

import (
	"secretmodel"
	"sinkapi"
)

func Reviewed(token secretmodel.Token) {
	sinkapi.Record(token)
}
