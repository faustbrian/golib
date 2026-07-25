//go:build !windows

package metricnameconsumer

import (
	"labelmodel"
	"metricsink"
)

func RecordPlatform(name labelmodel.LabelName) {
	metricsink.Label(string(name)) // want `observability/dynamic-label-name: labelmodel.LabelName flows to metric label name metricsink.Label argument 1`
}
