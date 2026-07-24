package metricnameconsumer

import (
	labels "labelmodel"
	metric "metricsink"
)

func Record(name labels.LabelName) {
	metric.Label(string(name))                   // want `observability/dynamic-label-name: labelmodel.LabelName flows to metric label name metricsink.Label argument 1`
	metric.Generic[labels.LabelName](name)       // want `observability/dynamic-label-name: labelmodel.LabelName flows to metric label name metricsink.Generic argument 1`
	metric.Variadic("bounded", name)             // want `observability/dynamic-label-name: labelmodel.LabelName flows to metric label name metricsink.Variadic argument 2`
	metric.Meter{}.Record(string(name), "value") // want `observability/dynamic-label-name: labelmodel.LabelName flows to metric label name metricsink.Meter.Record argument 1`

	metric.Label("method")
	metric.Label(labels.Bucket("user"))
	indirect := metric.Label
	indirect(string(name))
}
