// Package consumer deliberately bypasses every configured tenancy boundary.
package consumer

import (
	"context"

	auditmemory "github.com/faustbrian/golib/pkg/audit/memory"
	cachememory "github.com/faustbrian/golib/pkg/cache/backend/memory"
	"github.com/faustbrian/golib/pkg/queue"
	telemetryotlp "github.com/faustbrian/golib/pkg/telemetry/otlp"
	"github.com/faustbrian/golib/pkg/tenancy"
	"github.com/faustbrian/golib/pkg/tenancy/testdata/analyzer/metrics"
	workflowpostgres "github.com/faustbrian/golib/pkg/workflow/postgres"
)

// Bypass contains one direct call for every policy-owned negative fixture.
func Bypass(tenant tenancy.TenantID) {
	_, _ = cachememory.New(cachememory.Config{})
	_, _ = auditmemory.New(auditmemory.Config{})
	_, _ = queue.NewQueue()
	_, _ = workflowpostgres.New(nil, workflowpostgres.Config{})
	_, _ = telemetryotlp.NewTraceExporter(nil, telemetryotlp.Config{})
	metrics.Label(tenant)
	_ = context.Background()
}
