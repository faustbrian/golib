package password_test

import (
	"context"
	"errors"
	"fmt"

	password "github.com/faustbrian/golib/pkg/password"
)

func ExampleService() {
	service, err := password.New(password.DefaultPolicy())
	if err != nil {
		panic(err)
	}
	encoded, err := service.Hash(context.Background(), []byte("synthetic example password"))
	if err != nil {
		panic(err)
	}
	result, err := service.Verify(context.Background(), []byte("synthetic example password"), encoded.String())
	if errors.Is(err, password.ErrMismatch) {
		fmt.Println("rejected")
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Match(), result.NeedsRehash())
	// Output: true false
}
