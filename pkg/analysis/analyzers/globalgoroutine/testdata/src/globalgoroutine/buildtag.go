//go:build !never

package globalgoroutine

var buildTagged = func() int {
	go work() // want `lifecycle/no-global-goroutine: package initializer starts a goroutine without caller-visible ownership`
	return 1
}()
