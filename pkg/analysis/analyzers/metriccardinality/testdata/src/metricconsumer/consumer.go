package metricconsumer

import (
	"fmt"
	labels "labelmodel"
	metric "metricsink"
)

func Record(user labels.UserID, requestPath labels.RequestPath, low labels.LowCardinality) {
	metric.Label(string(user)) // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.Label argument 1`
	metric.Positioned("bounded", user)
	metric.Generic(user)                                       // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.Generic argument 1`
	metric.Generic[labels.RequestPath](requestPath)            // want `observability/high-cardinality-label: labelmodel.RequestPath flows to metric label metricsink.Generic argument 1`
	metric.GenericPair[string, labels.UserID]("bounded", user) // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.GenericPair argument 2`
	metric.Variadic(user)
	metric.All(user)                             // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.All argument 1`
	metric.Variadic("labels", user, requestPath) // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.Variadic argument 2` `observability/high-cardinality-label: labelmodel.RequestPath flows to metric label metricsink.Variadic argument 3`
	metric.Meter{}.Record("user", user)          // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.Meter.Record argument 2`
	metric.Meter{}.Record(string(user), "bounded")
	meter := &metric.Meter{}
	meter.PointerRecord("user", user)                                // want `observability/high-cardinality-label: labelmodel.UserID flows to metric label metricsink.Meter.PointerRecord argument 2`
	metric.Meter{}.Record("user", &user)                             // want `observability/high-cardinality-label: \*labelmodel.UserID flows to metric label metricsink.Meter.Record argument 2`
	metric.Variadic("users", []labels.UserID{user})                  // want `observability/high-cardinality-label: \[\]labelmodel.UserID flows to metric label metricsink.Variadic argument 2`
	metric.Variadic("users", [1]labels.UserID{user})                 // want `observability/high-cardinality-label: \[1\]labelmodel.UserID flows to metric label metricsink.Variadic argument 2`
	metric.Variadic("users", map[labels.UserID]int{user: 1})         // want `observability/high-cardinality-label: map\[labelmodel.UserID\]int flows to metric label metricsink.Variadic argument 2`
	metric.Variadic("users", map[string]labels.UserID{"user": user}) // want `observability/high-cardinality-label: map\[string\]labelmodel.UserID flows to metric label metricsink.Variadic argument 2`

	metric.Label(labels.Bucket(user))
	metric.Label(string(low))
	_ = fmt.Sprint(user)
	indirect := metric.Label
	indirect(string(user))
	func() {}()
}
