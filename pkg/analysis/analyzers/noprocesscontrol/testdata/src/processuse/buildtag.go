//go:build go1.1

package processuse

func BuildTaggedPanic() {
	panic("boom") // want `lifecycle/no-process-control: panic is restricted to approved entrypoints`
}
