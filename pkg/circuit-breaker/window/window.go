// Package window provides bounded rolling outcome data structures.
package window

const (
	// MaxCountSize bounds the record ring allocated by NewCount.
	MaxCountSize = 1 << 20
	// MaxBucketCount bounds the aggregate bucket ring allocated by NewTime.
	MaxBucketCount = 1 << 16
)

// Class is the mutually exclusive classification of a completion.
type Class uint8

const (
	Success Class = iota
	Failure
	Ignored
)

// Record is one completed operation without retaining its result or error.
type Record struct {
	Class Class
	Slow  bool
}

// Snapshot is an aggregate of the records currently retained by a window.
type Snapshot struct {
	Classified  uint64
	Successes   uint64
	Failures    uint64
	Ignored     uint64
	SlowSuccess uint64
	SlowFailure uint64
}

func (s *Snapshot) add(record Record) {
	switch record.Class {
	case Success:
		s.Classified++
		s.Successes++
		if record.Slow {
			s.SlowSuccess++
		}
	case Failure:
		s.Classified++
		s.Failures++
		if record.Slow {
			s.SlowFailure++
		}
	case Ignored:
		s.Ignored++
	}
}

func (s *Snapshot) remove(record Record) {
	switch record.Class {
	case Success:
		s.Classified--
		s.Successes--
		if record.Slow {
			s.SlowSuccess--
		}
	case Failure:
		s.Classified--
		s.Failures--
		if record.Slow {
			s.SlowFailure--
		}
	}
}

func valid(record Record) bool {
	return record.Class <= Ignored
}
