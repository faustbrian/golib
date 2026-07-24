package globalgoroutine

func work() {}

var started = func() int {
	go work() // want `lifecycle/no-global-goroutine: package initializer starts a goroutine without caller-visible ownership`
	return 1
}()

var nested = func() int {
	(func() {
		go work() // want `lifecycle/no-global-goroutine: package initializer starts a goroutine without caller-visible ownership`
	})()
	return 1
}()

var deferred = func() int {
	defer func() {
		go work() // want `lifecycle/no-global-goroutine: package initializer starts a goroutine without caller-visible ownership`
	}()
	return 1
}()

type box[T any] struct {
	value T
}

var generic = func() box[int] {
	go work() // want `lifecycle/no-global-goroutine: package initializer starts a goroutine without caller-visible ownership`
	return box[int]{value: 1}
}()

type startedAlias = int

var aliased = func() startedAlias {
	go work() // want `lifecycle/no-global-goroutine: package initializer starts a goroutine without caller-visible ownership`
	return 1
}()

var stored = func() {
	go work()
}

func indirect() int {
	go work()
	return 1
}

var indirectResult = indirect()

var callbacks = []func(){func() {
	go work()
}}
