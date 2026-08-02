// Package faultinject provides deterministic, bounded fault injection for Go
// tests and explicitly wired controlled experiments.
//
// A zero Injector is disabled. Faults can only be selected by an Injector
// returned by New from an explicit, validated configuration.
package faultinject
