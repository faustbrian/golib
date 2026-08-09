package tenancy_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func Example() {
	id, err := tenancy.ParseTenantID("customer-42")
	if err != nil {
		panic(err)
	}
	scope, err := tenancy.NewTenantScope(id, tenancy.Metadata{})
	if err != nil {
		panic(err)
	}
	ctx, err := tenancy.WithScope(context.Background(), scope)
	if err != nil {
		panic(err)
	}
	resolved, err := tenancy.RequireTenant(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(resolved.Equal(id))
	// Output: true
}

func ExampleNamespaceEncoder() {
	encoder, err := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		panic(err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("customer-42"), tenancy.Metadata{})
	key, err := encoder.Encode(scope, tenancy.NamespaceCache, "orders/17")
	if err != nil {
		panic(err)
	}
	fmt.Println(len(key), key[:4])
	// Output: 47 tn1_
}
