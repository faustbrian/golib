package metricunconfigured

import (
	"labelmodel"
	"metricsink"
)

func Record(user labelmodel.UserID) {
	metricsink.Label(string(user))
}
