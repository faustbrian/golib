// Package resilience composes explicit synchronous resilience policies and
// owns process-local retry-plus-hedge amplification accounting.
//
// The package deliberately does not implement retry, hedge, circuit-breaker,
// rate-limit, timeout, fallback, bulkhead, semaphore, or cache algorithms.
// Focused packages implement those decisions and adapt them through Policy,
// Stage, Execution, and WorkBudget.
//
// Policies are ordered outer-to-inner. Logical policies must precede attempt
// policies so a logical policy can invoke the attempt stack repeatedly while
// each physical attempt remains independently observable and budgeted.
// Execution is synchronous and never moves an operation into a goroutine.
package resilience
