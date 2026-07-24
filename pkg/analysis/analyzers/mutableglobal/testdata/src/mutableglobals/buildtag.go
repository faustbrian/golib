//go:build !windows

package mutableglobals

var buildState = []int{} // want `safety/no-mutable-global: package variable buildState holds shared mutable state`
