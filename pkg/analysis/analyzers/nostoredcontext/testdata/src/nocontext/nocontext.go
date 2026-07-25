package nocontext

import "fmt"

type safe struct {
	name string
}

var _ = fmt.Sprint
