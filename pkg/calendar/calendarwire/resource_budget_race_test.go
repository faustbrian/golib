//go:build race

package calendarwire_test

// The race detector adds two allocations around encoding/json internals.
const raceAllocationSlack = 2
