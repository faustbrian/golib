package mutableglobals

import (
	errorsalias "errors"
	"stateapi"
	"unsafe"
)

type scalar int
type scalarAlias = int
type errorAlias = error
type structure struct{ Value int }
type slice []int
type sliceAlias = []int
type service interface{ Serve() }
type customError interface{ Error() string }

const constant = 1

var scalarValue int
var namedScalar scalar
var aliasedScalar scalarAlias
var text = "accepted"
var sentinel error = errorsalias.New("accepted sentinel")
var inferredSentinel = errorsalias.New("accepted sentinel")
var aliasedSentinel errorAlias = errorsalias.New("accepted sentinel")

var pointer *int                 // want `safety/no-mutable-global: package variable pointer holds shared mutable state`
var values []int                 // want `safety/no-mutable-global: package variable values holds shared mutable state`
var namedSlice slice             // want `safety/no-mutable-global: package variable namedSlice holds shared mutable state`
var aliasedSlice sliceAlias      // want `safety/no-mutable-global: package variable aliasedSlice holds shared mutable state`
var mapping map[string]int       // want `safety/no-mutable-global: package variable mapping holds shared mutable state`
var events chan int              // want `safety/no-mutable-global: package variable events holds shared mutable state`
var callback func()              // want `safety/no-mutable-global: package variable callback holds shared mutable state`
var dependency service           // want `safety/no-mutable-global: package variable dependency holds shared mutable state`
var replaceableError customError // want `safety/no-mutable-global: package variable replaceableError holds shared mutable state`
var record structure             // want `safety/no-mutable-global: package variable record holds shared mutable state`
var items [1]int                 // want `safety/no-mutable-global: package variable items holds shared mutable state`
var generic stateapi.Box[int]    // want `safety/no-mutable-global: package variable generic holds shared mutable state`
var raw unsafe.Pointer           // want `safety/no-mutable-global: package variable raw holds shared mutable state`

var mixed, accepted = []int{}, 1 // want `safety/no-mutable-global: package variable mixed holds shared mutable state`
var _, tupleScalar = pair()

func pair() ([]int, int) { return nil, 0 }

func localState() {
	local := []int{}
	_ = local
}
