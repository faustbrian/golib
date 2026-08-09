# workflow

`workflow` provides explicit building blocks for durable business workflows and
sagas. Definitions have stable names and immutable versions, and instances are
expected to persist the exact definition name, version, and fingerprint that
created them.

The module is under active development. The current API covers definition
compilation and explicit version migrations; durable instance execution,
PostgreSQL storage, workers, activities, compensation, signals, timers,
operators, and optional integrations are not yet delivered.

The package does not claim exactly-once external side effects. Applications
must make activities idempotent and treat unknown outcomes as requiring
reconciliation before retry.

## Definition example

```go
definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
	Name:    "order.fulfillment",
	Version: "1",
	Mode:    workflow.Orchestration,
	Steps: []workflow.StepSpec{{
		Name:        "reserve",
		Kind:        workflow.StepActivity,
		Target:      "inventory.reserve",
		Timeout:     time.Minute,
		InputLimit:  16 << 10,
		ResultLimit: 16 << 10,
		Retry: workflow.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		},
	}},
})
if err != nil {
	return err
}

registry, err := workflow.CompileDefinitions(definition)
```

Definitions are copied at construction and registry compilation rejects a
duplicate name/version pair. A new version never changes a running instance;
applications must select and durably persist an explicit migration edge.
