package metricconsumer

import (
	"labelmodel"
	. "metricsink"
)

func RecordDot(user labelmodel.UserID) {
	Label(string(user)) // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.Label argument 1`
}
