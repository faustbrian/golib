package fanout

func Alias[T any](values []T) {
	for range values {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}
