package contextstore

import contextalias "context"

type Alias = contextalias.Context

type service struct {
	ctx Alias // want `context/no-stored-context: struct field stores a context lifecycle`
}

type embedded struct {
	contextalias.Context // want `context/no-stored-context: struct field stores a context lifecycle`
}

type box[T contextalias.Context] struct {
	value T // want `context/no-stored-context: struct field stores a context lifecycle`
}

type safe struct {
	cancel   contextalias.CancelFunc
	contexts []contextalias.Context
	name     string
}
