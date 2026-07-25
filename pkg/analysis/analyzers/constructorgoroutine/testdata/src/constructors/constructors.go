package constructors

func work() {}

func NewService() int {
	go work() // want `lifecycle/no-constructor-goroutine: constructor NewService starts a goroutine without caller-visible ownership`
	return 1
}

func NewGeneric[T any](value T) T {
	go work() // want `lifecycle/no-constructor-goroutine: constructor NewGeneric starts a goroutine without caller-visible ownership`
	return value
}

func NewConditional(enabled bool) int {
	if enabled {
		go work() // want `lifecycle/no-constructor-goroutine: constructor NewConditional starts a goroutine without caller-visible ownership`
	}
	return 1
}

func NewImmediate() int {
	(func() {
		go work() // want `lifecycle/no-constructor-goroutine: constructor NewImmediate starts a goroutine without caller-visible ownership`
	})()
	return 1
}

func NewDeferred() int {
	defer func() {
		go work() // want `lifecycle/no-constructor-goroutine: constructor NewDeferred starts a goroutine without caller-visible ownership`
	}()
	return 1
}

func NewStoredCallback() func() {
	return func() {
		go work()
	}
}

type Builder struct{}

func (*Builder) Build() int {
	go work() // want `lifecycle/no-constructor-goroutine: constructor Builder.Build starts a goroutine without caller-visible ownership`
	return 1
}

func helper() {
	go work()
}
