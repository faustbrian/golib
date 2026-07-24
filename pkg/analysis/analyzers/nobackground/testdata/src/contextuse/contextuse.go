package contextuse

import contextalias "context"

func BackgroundUse() contextalias.Context {
	return contextalias.Background() // want `context/no-background: context.Background is restricted to approved roots`
}

func TODOUse() contextalias.Context {
	return contextalias.TODO() // want `context/no-background: context.TODO is restricted to approved roots`
}

func Detached(parent contextalias.Context) contextalias.Context {
	return contextalias.WithoutCancel(parent) // want `context/no-background: context.WithoutCancel is restricted to approved roots`
}

func GenericRoot[T any](value T) contextalias.Context {
	_ = value
	return contextalias.Background() // want `context/no-background: context.Background is restricted to approved roots`
}

type provider struct{}

func (provider) Background() contextalias.Context {
	return nil
}

type callbacks struct {
	Background    func() contextalias.Context
	WithoutCancel func(contextalias.Context) contextalias.Context
}

func NearMiss(value provider) contextalias.Context {
	func() {}()
	callbacks{Background: func() contextalias.Context { return nil }}.Background()
	callbacks{WithoutCancel: func(parent contextalias.Context) contextalias.Context {
		return parent
	}}.WithoutCancel(contextalias.Background()) // want `context/no-background: context.Background is restricted to approved roots`
	derived, cancel := contextalias.WithCancel(value.Background())
	cancel()
	_ = derived
	return value.Background()
}
