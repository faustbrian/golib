package metricnameconsumer

import (
	"labelmodel"
	. "metricsink"
)

func RecordDot(name labelmodel.LabelName) {
	Label(string(name)) // want `observability/dynamic-label-name: labelmodel.LabelName flows to metric label name metricsink.Label argument 1`
}
