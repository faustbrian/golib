//go:build go1.1

package contextuse

import "context"

func BuildTaggedDetached(parent context.Context) context.Context {
	return context.WithoutCancel(parent) // want `context/no-background: context.WithoutCancel is restricted to approved roots`
}
