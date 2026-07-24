//go:build !windows

package fanout

func Platform(values []int) {
	for range values {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}
