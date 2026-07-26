package contextuse

import . "context"

func RootDot() Context {
	return TODO() // want `context/no-background: context.TODO is restricted to approved roots`
}

func DetachedDot(parent Context) Context {
	return WithoutCancel(parent) // want `context/no-background: context.WithoutCancel is restricted to approved roots`
}
