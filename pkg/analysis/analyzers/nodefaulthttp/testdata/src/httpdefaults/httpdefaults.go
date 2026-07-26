package httpdefaults

import httpalias "net/http"

func Defaults() {
	_ = httpalias.DefaultClient    // want `http/no-default-client: http.DefaultClient has shared implicit lifecycle and timeout policy`
	_ = httpalias.DefaultTransport // want `http/no-default-client: http.DefaultTransport has shared implicit lifecycle and timeout policy`
}

func GenericDefault[T any](value T) (*httpalias.Client, T) {
	return httpalias.DefaultClient, value // want `http/no-default-client: http.DefaultClient has shared implicit lifecycle and timeout policy`
}

func Explicit() {
	client := &httpalias.Client{Timeout: 1}
	transport := &httpalias.Transport{}
	cloned := httpalias.DefaultTransport.(*httpalias.Transport).Clone()
	_ = client
	_ = transport
	_ = cloned
	_ = httpalias.NoBody
}

type localDefaults struct {
	DefaultClient    *httpalias.Client
	DefaultTransport httpalias.RoundTripper
}

type cloneValue int

func (cloneValue) Clone() cloneValue { return 0 }

type clonePointer struct{}

func (*clonePointer) Clone() *clonePointer { return nil }

func cloneFactory() any { return nil }

func CloneNearMisses(value any, local *clonePointer) {
	_ = func() int { return 0 }()
	_ = func(value int) int { return value }(1)
	_ = local.Clone()
	_ = value.(cloneValue).Clone()
	_ = value.(*clonePointer).Clone()
	_ = cloneFactory().(*httpalias.Transport).Clone()
	_ = value.(*httpalias.Transport).Clone()
}

func NearMiss(local localDefaults) {
	_ = local.DefaultClient
	_ = local.DefaultTransport
}
