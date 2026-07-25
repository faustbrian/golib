//go:build go1.1

package constructors

func NewBuildTagged() int {
	go work() // want `lifecycle/no-constructor-goroutine: constructor NewBuildTagged starts a goroutine without caller-visible ownership`
	return 1
}
