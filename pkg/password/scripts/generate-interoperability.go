//go:build ignore

package main

import (
	"context"
	"fmt"
	"strings"

	password "github.com/faustbrian/golib/pkg/password"
)

func main() {
	argon, err := password.NewTestService(password.DefaultPolicy(), strings.NewReader(strings.Repeat("a", 64)))
	if err != nil {
		panic(err)
	}
	argonHash, err := argon.Hash(context.Background(), []byte("synthetic password"))
	if err != nil {
		panic(err)
	}
	limits := password.DefaultPolicy().Limits()
	bcryptPolicy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: 10, Limits: limits})
	if err != nil {
		panic(err)
	}
	bcryptService, err := password.New(bcryptPolicy)
	if err != nil {
		panic(err)
	}
	bcryptHash, err := bcryptService.Hash(context.Background(), []byte("synthetic password"))
	if err != nil {
		panic(err)
	}
	fmt.Println(argonHash.String())
	fmt.Println(bcryptHash.String())
}
