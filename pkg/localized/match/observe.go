package match

// Operation identifies a bounded resolution operation.
type Operation uint8

const (
	// OperationMatch identifies standards-aligned preference matching.
	OperationMatch Operation = iota
	// OperationFallback identifies configured fallback resolution.
	OperationFallback
)

// Event contains bounded low-cardinality resolution telemetry. It intentionally
// excludes requested, resolved, and content values.
type Event struct {
	Operation      Operation
	Kind           Kind
	CandidateCount int
}

// Observer receives completed resolution events and cannot alter outcomes.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe implements Observer.
func (function ObserverFunc) Observe(event Event) { function(event) }

func notify(observer Observer, event Event) {
	if observer == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		observer.Observe(event)
	}()
}
