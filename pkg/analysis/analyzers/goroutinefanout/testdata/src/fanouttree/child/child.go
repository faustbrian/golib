package child

func Fanout(values []int) {
	for range values {
		go func() {}() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}
