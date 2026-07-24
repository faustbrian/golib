//go:build !windows

package metricconsumer

import (
	"labelmodel"
	"metricsink"
)

func RecordPlatform(path labelmodel.RequestPath) {
	metricsink.Label(string(path)) // want `observability/high-cardinality-label: labelmodel.RequestPath flows to metric label metricsink.Label argument 1`
}
