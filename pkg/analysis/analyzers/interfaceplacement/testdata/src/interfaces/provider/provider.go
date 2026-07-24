package provider

import port "interfaces/portapi"

// Client is provider-owned and should instead be declared by a consumer.
type Client interface { // want `api/interface-placement: exported interface Client is declared in a configured provider package`
	Call()
}

// Empty is still a provider-owned value interface.
type Empty interface{} // want `api/interface-placement: exported interface Empty is declared in a configured provider package`

// Embedded inherits a consumer interface but is itself provider-owned.
type Embedded interface { // want `api/interface-placement: exported interface Embedded is declared in a configured provider package`
	port.Base
}

// Alias is an exported alias of a value interface.
type Alias = interface{ Close() } // want `api/interface-placement: exported interface Alias is declared in a configured provider package`

type internal interface{ hidden() }

// Numeric is a constraint, not a runtime value interface.
type Numeric interface {
	~int | ~int64
}

// CallableNumber remains constraint-only despite declaring a method.
type CallableNumber interface {
	Call()
	~int
}

// Box proves generic constraint declarations remain accepted.
type Box[T Numeric] struct{ Value T }

// Concrete is not an interface.
type Concrete struct{}

func localDeclaration() {
	type Local interface{ Call() }
	var _ Local
}
