package provider

import "interfaces/portapi"

type Client interface { // want `api/interface-naming: exported interface Client must start with Order and end with Port`
	Call()
}

type OrderClient interface { // want `api/interface-naming: exported interface OrderClient must start with Order and end with Port`
	Call()
}

type ClientPort interface { // want `api/interface-naming: exported interface ClientPort must start with Order and end with Port`
	Call()
}

type OrderClientPort interface {
	Call()
}

type Compatibility interface {
	Call()
}

type Legacy = portapi.Port // want `api/interface-naming: exported interface Legacy must start with Order and end with Port`

type OrderGenericPort[T any] interface {
	Accept(T)
}

type Number interface {
	~int | ~int64
}

type local interface {
	Call()
}

type Concrete struct{}
