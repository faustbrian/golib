package fleet

import (
	"sort"
	"sync"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
)

// QueueMetrics is a bounded current-state projection of lifecycle events.
// Runtime is cumulative so a metrics backend can derive honest averages and
// rates without this package retaining an unbounded time series.
type QueueMetrics struct {
	Enqueued  uint64
	Succeeded uint64
	Failed    uint64
	Runtime   time.Duration
}

// QueueSnapshot identifies one queue's current projected counters.
type QueueSnapshot struct {
	Backend string
	Queue   string
	Metrics QueueMetrics
}

// ProjectionSnapshot is an immutable point-in-time fleet projection.
type ProjectionSnapshot struct {
	Queues   []QueueSnapshot
	Overflow QueueMetrics
}

// Projection consumes queue lifecycle events without accessing a backend.
// New queue identities beyond the configured limit are aggregated into the
// overflow bucket to keep memory use bounded.
type Projection struct {
	mu        sync.RWMutex
	maxQueues int
	queues    map[queueKey]QueueMetrics
	overflow  QueueMetrics
}

type queueKey struct {
	backend string
	queue   string
}

// NewProjection creates a projection that retains at most maxQueues distinct
// queue identities. Non-positive limits retain no identities and aggregate all
// events into the overflow bucket.
func NewProjection(maxQueues int) *Projection {
	if maxQueues < 0 {
		maxQueues = 0
	}

	return &Projection{
		maxQueues: maxQueues,
		queues:    make(map[queueKey]QueueMetrics, maxQueues),
	}
}

// Observe implements queue.Observer.
func (p *Projection) Observe(event queue.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := queueKey{backend: event.Backend, queue: event.Queue}
	metrics, exists := p.queues[key]
	if !exists && len(p.queues) >= p.maxQueues {
		p.overflow.apply(event)

		return
	}

	metrics.apply(event)
	p.queues[key] = metrics
}

// Snapshot returns a deterministic copy safe for concurrent readers.
func (p *Projection) Snapshot() ProjectionSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snapshot := ProjectionSnapshot{
		Queues:   make([]QueueSnapshot, 0, len(p.queues)),
		Overflow: p.overflow,
	}
	for key, metrics := range p.queues {
		snapshot.Queues = append(snapshot.Queues, QueueSnapshot{
			Backend: key.backend,
			Queue:   key.queue,
			Metrics: metrics,
		})
	}
	sort.Slice(snapshot.Queues, func(i, j int) bool {
		if snapshot.Queues[i].Backend == snapshot.Queues[j].Backend {
			return snapshot.Queues[i].Queue < snapshot.Queues[j].Queue
		}

		return snapshot.Queues[i].Backend < snapshot.Queues[j].Backend
	})

	return snapshot
}

func (m *QueueMetrics) apply(event queue.Event) {
	switch event.Kind {
	case queue.EventEnqueued:
		m.Enqueued++
	case queue.EventHandlerSucceeded:
		m.Succeeded++
		m.Runtime += event.Duration
	case queue.EventHandlerFailed:
		m.Failed++
		m.Runtime += event.Duration
	default:
	}
}

var _ queue.Observer = (*Projection)(nil)
