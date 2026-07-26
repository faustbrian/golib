package window

import "fmt"

// Count retains a fixed number of the most recent records.
// Count is not safe for concurrent use; the owning breaker serializes access.
type Count struct {
	records  []Record
	next     int
	length   int
	snapshot Snapshot
}

// NewCount constructs a count-based window with fixed memory use.
func NewCount(size int) (*Count, error) {
	if size <= 0 {
		return nil, fmt.Errorf("window: count size must be greater than zero")
	}
	if size > MaxCountSize {
		return nil, fmt.Errorf("window: count size must not exceed %d", MaxCountSize)
	}

	return &Count{records: make([]Record, size)}, nil
}

// Add records one completion, evicting the oldest retained completion when full.
func (w *Count) Add(record Record) error {
	if !valid(record) {
		return fmt.Errorf("window: unknown outcome class %d", record.Class)
	}
	if record.Class == Ignored {
		w.snapshot.add(record)
		return nil
	}

	if w.length == len(w.records) {
		w.snapshot.remove(w.records[w.next])
	} else {
		w.length++
	}

	w.records[w.next] = record
	w.next = (w.next + 1) % len(w.records)
	w.snapshot.add(record)

	return nil
}

// Snapshot returns the current aggregate by value.
func (w *Count) Snapshot() Snapshot { return w.snapshot }
