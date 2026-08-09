// Package adapter is the only reviewed construction boundary in the tenancy
// analyzer fixture.
package adapter

import (
	auditmemory "github.com/faustbrian/golib/pkg/audit/memory"
	cachememory "github.com/faustbrian/golib/pkg/cache/backend/memory"
	"github.com/faustbrian/golib/pkg/queue"
	telemetryotlp "github.com/faustbrian/golib/pkg/telemetry/otlp"
	workflowpostgres "github.com/faustbrian/golib/pkg/workflow/postgres"
)

// Construct proves that the exact reviewed adapter exception remains narrow.
func Construct() {
	_, _ = cachememory.New(cachememory.Config{})
	_, _ = auditmemory.New(auditmemory.Config{})
	_, _ = queue.NewQueue()
	_, _ = workflowpostgres.New(nil, workflowpostgres.Config{})
	_, _ = telemetryotlp.NewTraceExporter(nil, telemetryotlp.Config{})
}
