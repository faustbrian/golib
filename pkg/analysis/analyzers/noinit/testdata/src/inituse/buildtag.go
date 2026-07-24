//go:build go1.1

package inituse

func init() { // want `lifecycle/no-init: package init hides construction and lifecycle ownership`
}
