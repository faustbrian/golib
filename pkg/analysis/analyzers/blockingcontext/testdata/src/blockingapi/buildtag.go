//go:build go1.1

package blockingapi

func BuildTagged(value string) error { // want `context/blocking-api-context: configured blocking API BuildTagged requires context.Context`
	return nil
}
