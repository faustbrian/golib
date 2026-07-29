package mpt

import "errors"

// ErrClosedStore identifies use of a closed storage adapter.
var ErrClosedStore = errors.New("mpt: closed store")
