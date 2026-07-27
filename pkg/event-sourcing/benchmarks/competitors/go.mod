module github.com/faustbrian/golib/pkg/event-sourcing/benchmarks/competitors

go 1.26.5

require (
	github.com/faustbrian/golib/pkg/event-sourcing v0.1.0
	github.com/hallgren/eventsourcing v0.9.1
	github.com/hallgren/eventsourcing/core v0.5.2
	github.com/looplab/eventhorizon v0.17.0
	github.com/thefabric-io/eventsourcing v0.6.0
)

require (
	github.com/aclements/go-moremath v0.0.0-20210112150236-f10218a38794 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/segmentio/ksuid v1.0.4 // indirect
	golang.org/x/perf v0.0.0-20260709024250-82a0b07e230d // indirect
)

tool golang.org/x/perf/cmd/benchstat
